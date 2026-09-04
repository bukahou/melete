package account

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// ⛔ 认证绕过回归测试 —— cross-exam 005 实施期，geass-v3 在自己实现里发现，
// 三家共用同一份设计，同款代换。
//
// 缺陷形态：用户不存在时把 hash 换成 dummy，然后【把比对结果当认证结论】。
// 于是拿 dummy 的明文当口令，可以登入【任何不存在的用户名】。
//
// ⚠️ 本案第四节那四条判据（唯一调用点 · 错误坍缩 · 绝对下界 · 内容哨兵）
// 一条都看不见它 —— 它们全都只管「时序与响应可不可分辨」，
// 而这条问的是「代换之后的比对结果能不能用来通过认证」。
// 为解决 A 引入的手段制造了 B，而检查 A 的判据看不见 B。
//
// melete 靠的是 VerifyPassword 里 `a == nil ||` 这个前置判断。
// 本测试把 dummyHash 换成【明文已知】的 hash，再拿那个明文去登录，
// 断言仍然失败 —— 若哪天有人「简化」掉那个判断，这里必红。
func TestDummyPlaintextCannotAuthenticate(t *testing.T) {
	const known = "dummy-plaintext-known-to-attacker"
	h, err := bcrypt.GenerateFromPassword([]byte(known), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	orig := dummyHash
	dummyHash = string(h)
	t.Cleanup(func() { dummyHash = orig })

	// 先确认这个测试有鉴别力：那个明文确实能通过 bcrypt 比对
	if bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(known)) != nil {
		t.Fatal("测试构造失败：明文对不上 dummy hash，这个测试证明不了任何事")
	}

	svc := NewService(&probeRepo{acct: nil}) // 用户不存在
	got, err := svc.VerifyPassword(context.Background(), "does-not-exist", known)
	if err == nil || got != nil {
		t.Fatalf("🔴 认证绕过：拿 dummy 明文登入了不存在的用户名 %+v", got)
	}
	if err != ErrBadCredentials {
		t.Errorf("错误应为 ErrBadCredentials，实际 %v", err)
	}
}

// 无本地密码的联邦账号同理：它也走 dummy 代换。
func TestDummyPlaintextCannotAuthenticateSSOAccount(t *testing.T) {
	const known = "dummy-plaintext-known-to-attacker"
	h, _ := bcrypt.GenerateFromPassword([]byte(known), bcrypt.MinCost)
	orig := dummyHash
	dummyHash = string(h)
	t.Cleanup(func() { dummyHash = orig })

	svc := NewService(&probeRepo{acct: &Account{}}) // 存在但 PasswordHash 为 nil
	got, err := svc.VerifyPassword(context.Background(), "sso-user", known)
	if err == nil || got != nil {
		t.Fatalf("🔴 认证绕过：拿 dummy 明文登入了纯 SSO 账号 %+v", got)
	}
}

// ⛔ 源码里不得出现 bcrypt hash 字面量。
//
// ⚠️ 这条是补上来的：原来只断言「生成器是随机的」，于是把
// `var dummyHash = mustGenerateDummyHash()` 改回源码常量，测试照样全绿 ——
// 生成器仍然随机，只是没人用它了。2026-09-04 变异测试实测过。
//
// 「dummy 不是硬编码」是【源码层】的性质，就用源码层的检查：
// 行为测试看不出一个 hash 是算出来的还是抄进去的，两者跑起来一模一样。
func TestNoHardcodedBcryptHashInSource(t *testing.T) {
	// ⚠️ 用 AST 而不是文本匹配：第一版按行 grep，把注释里举例说明的
	// `"$2a$10$"` 当成了违规。注释是给人读的，正是应该出现这种例子的地方。
	// 只看真正的字符串字面量，注释与标识符一概不看。
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go") // 测试里可以有：它要构造「明文已知」的 hash
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^\$2[aby]\$\d\d\$`)
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err == nil && re.MatchString(v) {
					t.Errorf("⛔ %s:%d 出现 bcrypt hash 字面量 —— dummy 必须运行时生成，"+
						"源码常量的明文是公开的", name, fset.Position(lit.Pos()).Line)
				}
				return true
			})
		}
	}
}

// 启动时生成的 dummy 每次进程都不同 —— 「知道明文」这件事本身不成立。
func TestGeneratedDummyIsNotAConstant(t *testing.T) {
	a, b := mustGenerateDummyHash(), mustGenerateDummyHash()
	if a == b {
		t.Errorf("两次生成的 dummy 相同 —— 熵源有问题")
	}
	if c, err := bcrypt.Cost([]byte(dummyHash)); err != nil || c != bcrypt.DefaultCost {
		t.Errorf("dummy 的 cost = %d（err=%v），应与新账号一致（%d）", c, err, bcrypt.DefaultCost)
	}
}

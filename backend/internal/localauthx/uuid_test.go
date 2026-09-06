package localauthx

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRoundTrip(t *testing.T) {
	for range 200 {
		id, err := NewUserID()
		if err != nil {
			t.Fatalf("NewUserID: %v", err)
		}
		b, err := encodeID(id)
		if err != nil {
			t.Fatalf("encodeID(%q): %v", id, err)
		}
		if len(b) != 16 {
			t.Fatalf("encodeID 应产出 16 字节，实得 %d", len(b))
		}
		back, err := decodeID(b)
		if err != nil {
			t.Fatalf("decodeID: %v", err)
		}
		if back != id {
			t.Fatalf("往返不一致：%q → %q", id, back)
		}
	}
}

// ⚠️ 宽松解析是本方案唯一的失败模式（见 uuid.go 顶部）：同一个 id 有多种
// 文本形态时，两个 string 不相等而类型系统看不出来，症状是「查不到这个用户」。
// 所以非 canonical 形式必须【被拒绝】，⛔ 不是被归一化后放行。
func TestRejectsNonCanonical(t *testing.T) {
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("NewV7: %v", err)
	}
	canonical := u.String()

	for name, bad := range map[string]string{
		"大写":      strings.ToUpper(canonical),
		"去掉连字符":   strings.ReplaceAll(canonical, "-", ""),
		"花括号":     "{" + canonical + "}",
		"urn 前缀":  "urn:uuid:" + canonical,
	} {
		t.Run(name, func(t *testing.T) {
			// 前提自检：这些写法 uuid.Parse 【本来是接受的】——
			// 若哪天上游收紧了解析，本测试就不再测「我们额外做的那道检查」，
			// ⛔ 它会变成一个恒绿的空测试。这里让那种情况直接报出来。
			if _, err := uuid.Parse(bad); err != nil {
				t.Skipf("上游 uuid.Parse 已拒绝 %q，本用例失去意义（需重写）", bad)
			}
			if _, err := encodeID(bad); err == nil {
				t.Fatalf("encodeID 接受了非 canonical 形式 %q —— 它会产生一个与 %q 不相等的 string", bad, canonical)
			}
		})
	}
}

func TestDecodeRejectsWrongLength(t *testing.T) {
	for _, n := range []int{0, 15, 17, 32} {
		if _, err := decodeID(make([]byte, n)); err == nil {
			t.Fatalf("decodeID 接受了 %d 字节 —— 静默截断会产生一个「看起来像 id 的 id」", n)
		}
	}
}

// ⭐ v7 是【为了时间有序】才选的（索引右端插入，避免 v4 的页分裂）。
// 这条测试守的是那个性质本身 —— 有人把 NewV7 换成 NewRandom 时它必须变红。
func TestIDsAreTimeOrdered(t *testing.T) {
	const n = 300
	got := make([]string, n)
	for i := range got {
		id, err := NewUserID()
		if err != nil {
			t.Fatalf("NewUserID: %v", err)
		}
		got[i] = id
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("生成的 id 不是字典序递增 —— v7 的时间有序性没有成立")
	}

	// ⛔ 判别力对照：随机 v4 必须【测不过】这条断言。
	//
	// ⚠️ 没有这一段，上面那个 sort 检查可能只是在描述一个恒真的东西 ——
	// 而「恒绿的测试」正是本项目已经吃过账的形态（P3 的基线取自被测对象）。
	v4 := make([]string, n)
	for i := range v4 {
		v4[i] = uuid.New().String()
	}
	if sort.StringsAreSorted(v4) {
		t.Fatal("对照组：随机 v4 竟然有序 —— 本测试不具判别力，断言需要重写")
	}
}

// ⛔⛔ 守门：UUID 的编解码只允许出现在本包的 uuid.go 里。
//
// ⚠️ 这条不是风格检查。散落的转换里只要有一处用了不同的字面形式，
// 它产生的 string 与别处的就不再相等，而两边都是 string、编译通过、
// 运行时表现为「查不到这个用户」—— 没有任何一层会报错。
// 让缺陷【写不出来】比让它可被发现更硬。
func TestUUIDConversionHasSingleHome(t *testing.T) {
	const pkg = "github.com/google/uuid"
	// ⚠️ 白名单只放【真正在做编解码】的那一处，外加两个测试文件。
	//
	// 2026-09-06 本测试拦下了 session_store_integration_test.go —— 那是它
	// 正常工作的样子。放行它之前想清楚了理由，⛔ 不是反射性加白名单：
	//   · 它只用 uuid 【生成】契约测试要的确定性 id（NewSHA1 名字空间 UUID），
	//     产出的是 canonical 文本，⛔ 从不接触 BINARY(16)
	//   · 真正的编解码仍然只发生在 uuid.go 的 encodeID / decodeID 里
	//
	// ⛔ 什么情况【不能】加白名单：某个文件自己把 string 转成 []byte 去写库，
	//    或自己拼 hex 去查库 —— 那正是本测试要挡的东西，
	//    「测试文件而已」不构成理由。
	allowed := map[string]bool{
		filepath.Join("internal", "localauthx", "uuid.go"):                          true,
		filepath.Join("internal", "localauthx", "uuid_test.go"):                     true,
		filepath.Join("internal", "localauthx", "session_store_integration_test.go"): true,
	}

	root, err := filepath.Abs("../..") // backend/
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	seen := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // 解析不了的文件不是本测试的职责
		}
		for _, im := range f.Imports {
			p, uerr := strconv.Unquote(im.Path.Value)
			if uerr != nil || p != pkg {
				continue
			}
			seen++
			if !allowed[rel] {
				offenders = append(offenders, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 判别力自检：一次都没扫到 import 说明遍历本身失效了（路径错、后缀错），
	// 那样这个测试会永远绿。⛔ 恒绿的守门等于没有守门。
	if seen == 0 {
		t.Fatalf("扫描没有发现任何 %s 的 import —— 遍历失效，本测试不具判别力（root=%s）", pkg, root)
	}
	if len(offenders) > 0 {
		t.Fatalf("这些文件也在直接用 %s：%v\n"+
			"⛔ 编解码只能在 internal/localauthx/uuid.go —— 见该文件顶部注释", pkg, offenders)
	}
}

// ast 在 ImportsOnly 模式下未被直接引用，保留 import 以便将来收紧到「检查具体调用」。
var _ = ast.Print

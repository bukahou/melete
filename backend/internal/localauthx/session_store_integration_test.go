package localauthx

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/bukahou/gokit/localauth/storetest"
	"github.com/google/uuid"

	"github.com/bukahou/melete/backend/internal/userid"
)

// ⭐ shim 已删（gokit v0.1.2 加了 SessionOptions.UserID）。
//
// 经过：v0.1.0 的套件用 rec("u1") 硬编码了 userID 形态，与模块自己
// 「不假设宿主 id 形态」的声明矛盾，melete 与 geass-v3 都过不了。
// melete 报出条目 → v0.1.1 加选项 → v0.1.2 由 geass-v3 对真实 MySQL store
// 跑套件实证。⛔ 我没有改模块的代码，只开了条目 —— 见接入计划 §6.3 的承诺。

// contractUserID 把契约里的逻辑名映射成 melete 接受的 canonical UUID。
//
// ⭐ 用名字空间 UUID（v5 = SHA-1）而不是维护一张 map：
// 它对同一个名字【永远给同一个 id】，且不同名字必不相同（套件要求单射），
// ⛔ 无状态、无锁、跨子测试稳定。
//
// ⚠️ 这里刻意【不用】v7：v7 带时间戳，同一个 "u1" 每次调用都会得到不同的 id，
// 而套件依赖「同一个逻辑名 = 同一个用户」——「登出其它设备」那条断言
// 会因此变成"两个不同用户"而无声通过。⛔ 那正是一条恒绿的假绿。
var contractNS = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

func contractUserID(name string) string {
	return uuid.NewSHA1(contractNS, []byte(name)).String()
}

func TestSessionStoreContractIntegration(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM user_sessions`) })
	storetest.RunSessionStoreTests(t, func(t *testing.T) localauth.SessionStore {
		// 每个子测试一张干净的表（套件要求）。
		// ⛔ 清全表在这里是安全的：user_sessions 是本次新建的，melete 现在的
		// 登录走的是 user_sessions（阶段 2 之前那张旧登录会话表已于阶段 6 退役）。
		if _, err := db.Exec(`DELETE FROM user_sessions`); err != nil {
			t.Fatal(err)
		}
		return NewSessionStore(db)
	}, storetest.SessionOptions{UserID: contractUserID})
}

// 单射自检：套件要求「不同逻辑名不得映到同一个 id」。
// ⚠️ 少了它，映射若退化成常量，「登出其它设备」「防 IDOR」两条断言
// 会因为"所有人是同一个人"而无声通过 —— 恒绿的假绿。
func TestContractUserIDIsInjective(t *testing.T) {
	seen := map[string]string{}
	for _, n := range []string{"u1", "u2", "someone-else", "u3"} {
		id := contractUserID(n)
		if prev, dup := seen[id]; dup {
			t.Fatalf("映射不是单射：%q 与 %q 都映到 %s", prev, n, id)
		}
		seen[id] = n
		if contractUserID(n) != id {
			t.Fatalf("映射不稳定：%q 两次调用结果不同", n)
		}
	}
}

func TestVerificationStoreContractIntegration(t *testing.T) {
	db := testDB(t)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM verification_tokens`) })
	storetest.RunVerificationStoreTests(t, func(t *testing.T) localauth.VerificationStore {
		if _, err := db.Exec(`DELETE FROM verification_tokens`); err != nil {
			t.Fatal(err)
		}
		return NewVerificationStore(db)
	}, storetest.VerificationOptions{
		// 我的 FindPending 在 SQL 里就滤掉了过期行 —— 守卫自己也会查
		// ExpiresAt，所以不滤也合法；滤了则少走一段路，并让「过期」与
		// 「不存在」在存储层归一。⛔ 声明为 true 才会跑那条额外断言。
		FiltersExpired: true,
	})
}

// 会话记下的客户端 IP 是文本形式：IPv6 最长 45 个字符（含 IPv4 映射的写法），列必须放得下，
// 否则这类客户端一登录就建不出会话（Data too long）。
func TestSessionStoreKeepsIPv6ClientIPIntegration(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := NewSessionStore(db)
	uid := contractUserID("ipv6-client")
	t.Cleanup(func() {
		if bin, err := userid.Encode(uid); err == nil {
			_, _ = db.Exec(`DELETE FROM user_sessions WHERE user_id = ?`, bin)
		}
	})
	now := time.Now().UTC().Truncate(time.Second)
	for i, ip := range []string{"2001:db8:85a3:0:0:8a2e:370:7334", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255"} {
		rec := localauth.SessionRecord{UserID: uid, ClientIP: ip, CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)}
		if _, err := store.Create(ctx, rec, []byte(fmt.Sprintf("%032d", i))); err != nil {
			t.Fatalf("IPv6 客户端（%d 字符）建会话失败：%v", len(ip), err)
		}
	}
	got, err := store.ListByUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	ips := map[string]bool{}
	for _, r := range got {
		ips[r.ClientIP] = true
	}
	if len(got) != 2 || !ips["ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255"] {
		t.Fatalf("读回的会话 IP 不对：%+v", got)
	}
}

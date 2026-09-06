package localauthx

import (
	"context"
	"errors"
	"testing"

	"github.com/bukahou/gokit/localauth"
	"github.com/jmoiron/sqlx"
)

// ⚠️ 这一组没有模块提供的契约套件（gokit 只为三个 Store 提供），
// 所以断言是我写的 —— ⛔ 判别力必须自己证明，见每条末尾的变异说明。

func newAccounts(t *testing.T) (*credentialStore, *sqlx.DB) {
	db := testDB(t)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE username LIKE 'it_%'`) })
	if _, err := db.Exec(`DELETE FROM users WHERE username LIKE 'it_%'`); err != nil {
		t.Fatal(err)
	}
	return NewCredentialStore(db), db
}

func TestCreateAccountDuplicatesIntegration(t *testing.T) {
	s, _ := newAccounts(t)
	ctx := context.Background()

	if _, err := s.CreateAccount(ctx, localauth.NewAccount{
		Username: "it_alice", Email: "it_alice@example.com", PasswordHash: "$2a$10$x", DisplayName: "A",
	}); err != nil {
		t.Fatalf("首次建号: %v", err)
	}

	// ⭐ 撞用户名 → CodeUsernameTaken，⛔ 不是 500。
	// 变异：删掉 duplicateKeyOf 的 uk_username 分支 → 这里拿到裸 error，变红。
	_, err := s.CreateAccount(ctx, localauth.NewAccount{Username: "it_alice", Email: "it_other@example.com"})
	assertCode(t, err, localauth.CodeUsernameTaken)

	// 🔴 撞邮箱 → CodeEmailTaken。
	// ⚠️ 这条同时在测【uk_email 唯一索引存在】—— 索引若不存在，
	// 这次插入会成功，两个账号共用一个已验证邮箱，找回密码会改错账号。
	_, err = s.CreateAccount(ctx, localauth.NewAccount{Username: "it_bob", Email: "it_alice@example.com"})
	assertCode(t, err, localauth.CodeEmailTaken)
}

func assertCode(t *testing.T, err error, want localauth.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望 %s，却成功了", want)
	}
	var le *localauth.Error
	if !errors.As(err, &le) {
		t.Fatalf("期望 localauth.Error(%s)，得到裸 error: %v", want, err)
	}
	if le.Code != want {
		t.Fatalf("错误码不对：want %s got %s", want, le.Code)
	}
}

// ⭐ 纯 OIDC 账号：没有口令是【合法且常见】的状态。
// Hash 空串 ≠ 未加载 ≠ 错误 —— 「能不能设初始口令」正是靠它判断。
func TestLoadCredentialOIDCAccountIntegration(t *testing.T) {
	s, _ := newAccounts(t)
	ctx := context.Background()
	id, err := s.CreateAccount(ctx, localauth.NewAccount{Username: "it_sso"})
	if err != nil {
		t.Fatal(err)
	}
	c, found, err := s.LoadCredential(ctx, id)
	if err != nil || !found {
		t.Fatalf("应找到账号: found=%v err=%v", found, err)
	}
	if c.Hash != "" {
		t.Fatalf("纯 OIDC 账号的 Hash 应为空串，得到 %q", c.Hash)
	}
	if !c.Active {
		t.Fatal("新建账号应可登录")
	}
}

// ⛔⛔ canLogin 必须是白名单。
//
// ⚠️ 变异验证：把 canLogin 改成 `status != StatusBanned`（黑名单），
// 「停用账号不可登录」这条立刻变红。一个同类系统实测栽过的缺陷就是这个
// （cross-exam 005 §27：黑名单判定导致停用账号仍可登录）。
func TestStatusIsWhitelistIntegration(t *testing.T) {
	s, db := newAccounts(t)
	ctx := context.Background()
	id, err := s.CreateAccount(ctx, localauth.NewAccount{Username: "it_status"})
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := encodeID(id)

	for name, status := range map[string]int{
		"停用":     StatusInactive,
		"封禁":     StatusBanned,
		"未知(零值)": StatusUnknown,
		"将来新增的状态": 99, // ⭐ 白名单的价值就在这一行：新状态默认被拒
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, uid); err != nil {
				t.Fatal(err)
			}
			c, _, err := s.LoadCredential(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if c.Active {
				t.Fatalf("status=%d 不该可登录 —— 白名单被写成黑名单了？", status)
			}
			st, err := s.AccountStatus(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if st.Active {
				t.Fatalf("AccountStatus 在 status=%d 时仍报 Active", status)
			}
		})
	}

	// 判别力对照：Active 状态必须【真的】可登录，
	// 否则上面全部通过也可能只是因为 canLogin 恒 false。
	if _, err := db.Exec(`UPDATE users SET status = ? WHERE id = ?`, StatusActive, uid); err != nil {
		t.Fatal(err)
	}
	c, _, _ := s.LoadCredential(ctx, id)
	if !c.Active {
		t.Fatal("对照组：StatusActive 应可登录 —— 本测试不具判别力")
	}
}

// 账号不存在时 ⛔ 不报错：报错会让「查不到」与「存储故障」可区分，
// 而前者是攻击者可探测的。
func TestAccountStatusUnknownUserIntegration(t *testing.T) {
	s, _ := newAccounts(t)
	ghost, _ := NewUserID()
	st, err := s.AccountStatus(context.Background(), ghost)
	if err != nil {
		t.Fatalf("不存在的账号不该报错: %v", err)
	}
	if st.Active {
		t.Fatal("不存在的账号不该是 Active")
	}
}

// ⭐ 多行 email=NULL 必须能共存 —— 否则加了 uk_email 之后
// 第二个纯 OIDC 账号就建不出来了。⚠️ 这条依赖 MySQL 的 UNIQUE 对 NULL
// 的语义，⛔ 换库要重验（本项目已有一条同类的 COLLATE 债）。
func TestNullEmailsCoexistIntegration(t *testing.T) {
	s, _ := newAccounts(t)
	ctx := context.Background()
	for _, u := range []string{"it_n1", "it_n2", "it_n3"} {
		if _, err := s.CreateAccount(ctx, localauth.NewAccount{Username: u}); err != nil {
			t.Fatalf("第二个无邮箱账号就该能建：%v", err)
		}
	}
}

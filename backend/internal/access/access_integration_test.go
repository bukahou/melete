package access

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// 三档用户（P9 #27–#30）。自造账号与题库、只删自己造的行 —— CI 的空表与 dev 库都能跑。
func TestAccessIntegration(t *testing.T) {
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过集成测试")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	svc := NewService(db)

	newUser := func(name string, perms ...string) userid.UserID {
		t.Helper()
		raw, err := userid.New()
		if err != nil {
			t.Fatal(err)
		}
		id := userid.UserID(raw)
		uname := fmt.Sprintf("t-acc-%s-%d", name, time.Now().UnixNano()%1e9)
		if _, err := db.Exec(`INSERT INTO users (id, username, status, created_at, updated_at, display_name)
		                      VALUES (?, ?, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP(), ?)`, id, uname, name); err != nil {
			t.Fatal(err)
		}
		for _, p := range perms {
			if _, err := db.Exec(`INSERT INTO user_permissions (user_id, permission, granted_at) VALUES (?, ?, UTC_TIMESTAMP())`, id, p); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			_, _ = db.Exec(`DELETE FROM user_permissions WHERE user_id = ?`, id)
			_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, id)
		})
		return id
	}
	tierOf := func(id userid.UserID) Tier {
		t.Helper()
		tier, _, err := svc.Resolve(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return tier
	}

	admin := newUser("admin", PermManage)
	admin2 := newUser("admin2", PermManage)
	super := newUser("super", PermAll)
	basic := newUser("basic")
	adv := newUser("adv", PermPrivate)

	// ① 档位推导：空集 = 普通；最高那一档说了算
	for _, c := range []struct {
		id   userid.UserID
		want Tier
	}{{admin, TierAdmin}, {super, TierAdmin}, {basic, TierBasic}, {adv, TierAdvanced}} {
		if got := tierOf(c.id); got != c.want {
			t.Fatalf("档位：得到 %s，期望 %s", got, c.want)
		}
	}

	// ② 过期的权限不算
	if _, err := db.Exec(`UPDATE user_permissions SET expires_at = UTC_TIMESTAMP() - INTERVAL 1 DAY WHERE user_id = ?`, adv); err != nil {
		t.Fatal(err)
	}
	if got := tierOf(adv); got != TierBasic {
		t.Fatalf("过期的 content:private 不应生效，得到 %s", got)
	}
	if _, err := db.Exec(`UPDATE user_permissions SET expires_at = NULL WHERE user_id = ?`, adv); err != nil {
		t.Fatal(err)
	}

	// ③ admin 升级普通 → 高级，记下是谁升的；再降回普通。两个方向都幂等
	for i := 0; i < 2; i++ {
		if err := svc.SetTier(ctx, admin, basic, TierAdvanced); err != nil {
			t.Fatal(err)
		}
	}
	if got := tierOf(basic); got != TierAdvanced {
		t.Fatalf("升级后应为 advanced，得到 %s", got)
	}
	var by []byte
	if err := db.Get(&by, `SELECT granted_by FROM user_permissions WHERE user_id = ? AND permission = ?`, basic, PermPrivate); err != nil {
		t.Fatal(err)
	}
	if got, _ := userid.Decode(by); got != string(admin) {
		t.Fatalf("granted_by 应是执行升级的 admin")
	}
	for i := 0; i < 2; i++ {
		if err := svc.SetTier(ctx, admin, basic, TierBasic); err != nil {
			t.Fatal(err)
		}
	}
	if got := tierOf(basic); got != TierBasic {
		t.Fatalf("降级后应为 basic，得到 %s", got)
	}

	// ④ admin 不能操作任何 admin：别的 admin、超级、以及自己
	for name, target := range map[string]userid.UserID{"别的 admin": admin2, "超级": super, "自己": admin} {
		if err := svc.SetTier(ctx, admin, target, TierAdvanced); !errors.Is(err, ErrTargetIsAdmin) {
			t.Fatalf("admin 操作%s应为 ErrTargetIsAdmin，得到 %v", name, err)
		}
		if err := svc.SetTier(ctx, admin, target, TierBasic); !errors.Is(err, ErrTargetIsAdmin) {
			t.Fatalf("admin 降级%s应为 ErrTargetIsAdmin，得到 %v", name, err)
		}
	}
	if got := tierOf(admin2); got != TierAdmin {
		t.Fatalf("别的 admin 的档位不应被改动，得到 %s", got)
	}

	// ⑤ 不接受 admin 作为目标档位 —— 应用里没有授予 admin 的入口
	if err := svc.SetTier(ctx, admin, basic, TierAdmin); !errors.Is(err, ErrInvalidTier) {
		t.Fatalf("设为 admin 应为 ErrInvalidTier，得到 %v", err)
	}

	// ⑥ 非 admin 调不动（纵深防御第二道）；目标不存在 ⇒ ErrNotFound
	if err := svc.SetTier(ctx, adv, basic, TierAdvanced); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("高级用户升级别人应为 ErrNotAdmin，得到 %v", err)
	}
	if _, _, err := svc.ListUsers(ctx, basic, 1, 10); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("普通用户列用户应为 ErrNotAdmin，得到 %v", err)
	}
	ghost, _ := userid.New()
	if err := svc.SetTier(ctx, admin, userid.UserID(ghost), TierAdvanced); !errors.Is(err, ErrNotFound) {
		t.Fatalf("目标不存在应为 ErrNotFound，得到 %v", err)
	}

	// ⑦ 用户列表带档位
	users, total, err := svc.ListUsers(ctx, admin, 1, 100)
	if err != nil || total < 5 {
		t.Fatalf("用户列表：total=%d err=%v", total, err)
	}
	seen := map[userid.UserID]Tier{}
	for _, u := range users {
		seen[u.ID] = u.Tier
	}
	if seen[adv] != TierAdvanced || seen[admin] != TierAdmin || seen[basic] != TierBasic {
		t.Fatalf("列表里的档位不对：%v", seen)
	}

	// ⑦' 自己的资料：显示名与档位
	if p, err := svc.Profile(ctx, adv); err != nil || p.DisplayName != "adv" || p.Tier != TierAdvanced {
		t.Fatalf("Profile 不对：%+v（%v）", p, err)
	}
	if _, err := svc.Profile(ctx, userid.UserID(ghost)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的账号应为 ErrNotFound，得到 %v", err)
	}

	// ⑧ 按题目编号查所属题库的可见性
	slug := fmt.Sprintf("t-acc-%d", time.Now().UnixNano()%1e9)
	r, err := db.Exec(`INSERT INTO bank (slug, name, locale, kind) VALUES (?, ?, 'zh', 'cert')`, slug, slug)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := r.LastInsertId()
	r, err = db.Exec(`INSERT INTO question (bank_id, external_no, stem, kind, pick_count) VALUES (?, 1, '题干', 'single', 1)`, bankID)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := r.LastInsertId()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM question WHERE bank_id = ?`, bankID)
		_, _ = db.Exec(`DELETE FROM bank WHERE id = ?`, bankID)
	})
	if v, err := svc.QuestionVisibility(ctx, qid); err != nil || v != VisibilityPrivate {
		t.Fatalf("新题库缺省应为 private，得到 %q（%v）", v, err)
	}
	if _, err := svc.QuestionVisibility(ctx, -1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的题目应为 ErrNotFound，得到 %v", err)
	}
}

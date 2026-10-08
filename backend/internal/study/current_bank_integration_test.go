package study

import (
	"context"
	"errors"
	"testing"

	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/userid"
)

// 当前题库（P9 #2 #14）与练习正确率的两个口径（P9 #9）。
//
// ⭐ 用 fixture 自造两个题库 —— ⛔ 不借库里已有的题：CI 的测试库是空表。
func TestCurrentBankIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	me, qA, slugA := fixture(t, db, "当前题库 A")
	other, qB, slugB := fixture(t, db, "当前题库 B")
	svc := NewService(db, question.NewMySQLRepository(db))

	check := func(step, slug string, src CurrentBankSource) {
		t.Helper()
		cur, err := svc.LoadCurrentBank(ctx, me)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if cur.Slug != slug || cur.Source != src {
			t.Fatalf("%s: 得到 (%q, %s)，期望 (%q, %s)", step, cur.Slug, cur.Source, slug, src)
		}
	}

	// ① 新账号：没选过、没作答 ⇒ none
	check("新账号", "", CurrentBankNone)

	// ② 先答 A 再答 B ⇒ recent = B。
	//    ⚠️ 把 A 那条挪早一分钟：同一秒内的两条靠 id 排，而 TiDB 的 id 不保证单调。
	mustRecordAs(t, ctx, svc, me, qA, "A")
	mustRecordAs(t, ctx, svc, me, qB, "B")
	if _, err := db.Exec(`UPDATE attempt SET created_at = created_at - INTERVAL 1 MINUTE
	                      WHERE user_id=? AND question_id=?`, me, qA); err != nil {
		t.Fatal(err)
	}
	check("有作答未选过", slugB, CurrentBankRecent)

	// ③ 选了 A ⇒ chosen 压过 recent
	if err := svc.ChooseCurrentBank(ctx, me, slugA); err != nil {
		t.Fatal(err)
	}
	check("选过 A", slugA, CurrentBankChosen)

	// ④ 选一个不存在的题库 ⇒ ErrNotFound，且原来的选择不变
	if err := svc.ChooseCurrentBank(ctx, me, "no-such-bank-p9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的题库应返回 ErrNotFound，得到 %v", err)
	}
	check("选不存在的题库之后", slugA, CurrentBankChosen)

	// ⑤ 选中的题库下架（指向不存在的 id）⇒ 视同没选过，退回 recent，⛔ 不返回打不开的 slug
	if _, err := db.Exec(`UPDATE users SET current_bank_id = -1 WHERE id=?`, me); err != nil {
		t.Fatal(err)
	}
	check("选中的题库下架", slugB, CurrentBankRecent)

	// ⑥ 隔离：另一个账号既没选过也没作答 ⇒ none（⛔ 不能看到 me 的选择或作答）
	cur, err := svc.LoadCurrentBank(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Source != CurrentBankNone {
		t.Fatalf("另一个账号应为 none，得到 (%q, %s) —— 串号了", cur.Slug, cur.Source)
	}

	// ⑦ 两个口径：B 先错后对 ⇒ 掌握率看最近一次（1/1），练习正确率看全部（1/2）
	mustRecordAs(t, ctx, svc, me, qB, "A")
	p, err := svc.LoadProgress(ctx, me, slugB)
	if err != nil {
		t.Fatal(err)
	}
	if p.SeenCount != 1 || p.CorrectCount != 1 || p.AttemptCount != 2 || p.AttemptCorrectCount != 1 {
		t.Fatalf("口径不对：seen=%d correct=%d attempts=%d attemptCorrect=%d，期望 1/1/2/1",
			p.SeenCount, p.CorrectCount, p.AttemptCount, p.AttemptCorrectCount)
	}
}

// mustRecordAs 按界面现在的走法记一次作答：⛔ 不带 rating（P9 #15 起界面没有自评）。
func mustRecordAs(t *testing.T, ctx context.Context, svc Service, who userid.UserID, q int64, chosen string) {
	t.Helper()
	if _, err := svc.RecordAttempt(ctx, Attempt{AccountID: who, QuestionID: q, Chosen: chosen}); err != nil {
		t.Fatalf("记录作答: %v", err)
	}
}

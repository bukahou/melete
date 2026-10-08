package study

import (
	"context"
	"errors"
	"testing"

	"github.com/bukahou/melete/backend/internal/question"
)

// 收藏（P9 #20）：两个方向幂等、不存在的题拒收、⭐ 另一个账号看不到我的收藏。
func TestBookmarkIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	me, q, _ := fixture(t, db, "收藏")
	other, _, _ := fixture(t, db, "收藏-另一个账号")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM bookmark WHERE question_id = ?`, q) })
	svc := NewService(db, question.NewMySQLRepository(db))

	is := func(step, who string, want bool) {
		t.Helper()
		var got bool
		var err error
		if who == "me" {
			got, err = svc.IsBookmarked(ctx, me, q)
		} else {
			got, err = svc.IsBookmarked(ctx, other, q)
		}
		if err != nil || got != want {
			t.Fatalf("%s: 得到 %v（%v），期望 %v", step, got, err, want)
		}
	}

	is("初始", "me", false)
	for i := 0; i < 2; i++ { // ⭐ 收藏两次 = 一次（幂等，不报重复键）
		if err := svc.SetBookmark(ctx, me, q, true); err != nil {
			t.Fatalf("第 %d 次收藏: %v", i+1, err)
		}
	}
	is("收藏后", "me", true)
	is("另一个账号", "other", false)

	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM bookmark WHERE question_id = ?`, q); err != nil || n != 1 {
		t.Fatalf("收藏两次应只有 1 行，得到 %d（%v）", n, err)
	}

	for i := 0; i < 2; i++ { // 取消两次也不报错
		if err := svc.SetBookmark(ctx, me, q, false); err != nil {
			t.Fatalf("第 %d 次取消: %v", i+1, err)
		}
	}
	is("取消后", "me", false)

	if err := svc.SetBookmark(ctx, me, -1, true); !errors.Is(err, ErrQuestionNotFound) {
		t.Fatalf("收藏不存在的题应 ErrQuestionNotFound，得到 %v", err)
	}
}

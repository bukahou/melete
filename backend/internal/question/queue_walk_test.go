package question

import (
	"context"
	"testing"
)

// 走完一条【会缩短的】队列必须不漏题。
//
// ⚠️ 这条编码的是前端的翻页策略，而缺陷正出在那里：
// 刷题页原本用 `offset = i`、点下一题就 `i+1`。但 due/wrong/unsure/unseen
// 四个模式的集合会随作答缩短 —— 做完 offset 0 那题它就离开集合，
// 原来的 offset 1 变成 offset 0，再取 offset 1 就漏了一题，而且不报错。
//
// 2026-09-04 实测：队列 [3,1,2]，做完 #3 后点下一题落到 #2，#1 被跳过。
// 修法是「答完之后回到 offset 0」（队列自己前进了）。本测试模拟修好之后的走法。
func TestWalkingShrinkingQueueVisitsEveryItem(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	bankID, acct := dueFixture(t, db)
	repo := NewMySQLRepository(db)

	answer := func(externalNo int) {
		t.Helper()
		if _, err := db.Exec(`UPDATE card c JOIN question q ON q.id = c.question_id
		                      SET c.due = UTC_TIMESTAMP() + INTERVAL 4 DAY
		                      WHERE c.account_id = ? AND q.external_no = ?`, acct, externalNo); err != nil {
			t.Fatal(err)
		}
	}

	var visited []int
	for step := 0; step < 10; step++ { // 上限只为防止写错时死循环
		p, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 1, Offset: 0, // ⭐ 永远取 offset 0
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) == 0 {
			break
		}
		no := p.Items[0].ExternalNo
		visited = append(visited, no)
		answer(no)
	}

	t.Logf("走过: %v", visited)
	want := []int{3, 1, 2} // 按到期先后：-10 天、-3 天、-1 天
	if len(visited) != len(want) {
		t.Fatalf("走过 %v，期望 %v —— 数量不对说明漏题或重复", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Errorf("第 %d 步是 #%d，期望 #%d（完整：%v vs %v）", i+1, visited[i], want[i], visited, want)
		}
	}
}

// 对照组：证明旧策略（offset 递增）确实会漏 —— 一个没见过红的修复不算修复。
func TestOldIncrementingOffsetSkips(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	bankID, acct := dueFixture(t, db)
	repo := NewMySQLRepository(db)

	var visited []int
	for i := 0; i < 3; i++ {
		p, err := repo.ListQuestions(ctx, bankID, ListFilter{
			Mode: "due", AccountID: acct, Limit: 1, Offset: i, // ⛔ 旧策略
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Items) == 0 {
			break
		}
		no := p.Items[0].ExternalNo
		visited = append(visited, no)
		if _, err := db.Exec(`UPDATE card c JOIN question q ON q.id = c.question_id
		                      SET c.due = UTC_TIMESTAMP() + INTERVAL 4 DAY
		                      WHERE c.account_id = ? AND q.external_no = ?`, acct, no); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("旧策略走过: %v（队列原本 [3 1 2]）", visited)
	if len(visited) == 3 {
		t.Errorf("旧策略居然没漏题？那说明这个测试构造不出缺陷，本文件的修复也就无从谈起")
	}
}

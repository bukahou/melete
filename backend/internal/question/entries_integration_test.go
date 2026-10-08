package question

import (
	"context"
	"slices"
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// P9 第 4 步的出题条件：卷子 / 题号段 / 标签并集 / 收藏 / 种子打乱 / 截取前 N 题，
// 以及「继续」的定位与本轮小结。
//
// 造两套卷子各 3 题：s1#1 s1#2 s1#3 · s2#1 s2#2 s2#3（两套都从 1 数起 —— 多套卷子的真实形状）。
//
//	标签 A：s1#1、s2#2      标签 B：s1#2、s2#2（⭐ s2#2 同时命中 A 和 B：并集里只能出现一次）
func TestDrillEntriesIntegration(t *testing.T) {
	db := openDueTestDB(t)
	ctx := context.Background()
	repo := NewMySQLRepository(db)
	svc := NewService(repo)

	bankID, q := entriesFixture(t, db)
	me := mkUser(t, db, "ent-"+randSuffix())
	other := mkUser(t, db, "ent-o-"+randSuffix())
	tagA := mkTag(t, db, bankID, "A", q["s1#1"], q["s2#2"])
	tagB := mkTag(t, db, bankID, "B", q["s1#2"], q["s2#2"])

	ids := func(f ListFilter) []int64 {
		t.Helper()
		f.AccountID, f.Limit = me, 100
		p, err := svc.ListQuestions(ctx, bankID, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, 0, len(p.Items))
		for _, it := range p.Items {
			out = append(out, it.ID)
		}
		if p.Total != len(out) {
			t.Fatalf("total=%d 与实际条数 %d 不一致", p.Total, len(out))
		}
		return out
	}
	want := func(step string, got []int64, keys ...string) {
		t.Helper()
		exp := make([]int64, len(keys))
		for i, k := range keys {
			exp[i] = q[k]
		}
		if !slices.Equal(got, exp) {
			t.Fatalf("%s: 得到 %v，期望 %v %v", step, got, keys, exp)
		}
	}

	s2 := "s2"
	want("卷子 s2", ids(ListFilter{Session: &s2}), "s2#1", "s2#2", "s2#3")
	// ⭐ 不给卷子时按 (卷子, 题号) 排 —— 只按题号会把两套交错成 1,1,2,2,3,3
	want("题号 2–3", ids(ListFilter{NoFrom: 2, NoTo: 3}), "s1#2", "s1#3", "s2#2", "s2#3")
	want("标签并集 A∪B", ids(ListFilter{AnyTagIDs: []int64{tagA, tagB}}), "s1#1", "s1#2", "s2#2")
	want("标签交集 A∩B（旧参数不变）", ids(ListFilter{TagIDs: []int64{tagA, tagB}}), "s2#2")

	// 收藏：⭐ 另一个账号也收藏一题 —— 漏了 user_id 条件它就会混进来
	mustExec(t, db, `INSERT INTO bookmark (user_id,question_id,created_at) VALUES (?,?,UTC_TIMESTAMP())`, me, q["s1#3"])
	mustExec(t, db, `INSERT INTO bookmark (user_id,question_id,created_at) VALUES (?,?,UTC_TIMESTAMP())`, other, q["s2#3"])
	want("收藏", ids(ListFilter{OnlyBookmarked: true}), "s1#3")
	// 列表上的「已收藏」标记（刷题页 / 4.3 列表的书签图标）：我只收藏了 s1#3
	if p, err := svc.ListQuestions(ctx, bankID, ListFilter{AccountID: me, Session: &s2, Limit: 10}); err != nil {
		t.Fatal(err)
	} else {
		for _, it := range p.Items {
			if it.Bookmarked {
				t.Fatalf("s2 里我没有收藏任何题，#%d 却标成已收藏 —— 串号了（s2#3 是另一个账号收藏的）", it.ExternalNo)
			}
		}
	}
	if _, err := svc.ListQuestions(ctx, bankID, ListFilter{OnlyBookmarked: true}); err != ErrModeNeedsAccount {
		t.Fatalf("未认证查收藏应被拒，得到 %v", err)
	}

	// 种子：同一个种子永远同一个顺序；换种子顺序不同。
	// ⚠️ 题目 id 每次运行都不同，所以「换种子顺序不同」不是确定性的 —— 6 题的随机排列撞车概率 1/720。
	//   CRC32 时代这条一半概率失败（线性哈希，换种子几乎不改变顺序），那正是它要抓的 bug。
	seed1, seed2 := int64(11), int64(12)
	a1, a2 := ids(ListFilter{Seed: &seed1}), ids(ListFilter{Seed: &seed1})
	if !slices.Equal(a1, a2) {
		t.Fatalf("同一种子两次顺序不同：%v / %v", a1, a2)
	}
	if b := ids(ListFilter{Seed: &seed2}); slices.Equal(a1, b) {
		t.Fatalf("两个种子给出了同一个顺序 %v —— 打乱没生效", a1)
	}
	natural := ids(ListFilter{})
	if slices.Equal(a1, natural) {
		t.Fatalf("打乱后与题号顺序相同 %v", a1)
	}

	// Take：集合 = 打乱后的前 4 题；翻页不能翻出集合
	took := ids(ListFilter{Seed: &seed1, Take: 4})
	if !slices.Equal(took, a1[:4]) {
		t.Fatalf("take=4 应是打乱顺序的前 4 题 %v，得到 %v", a1[:4], took)
	}
	p, err := svc.ListQuestions(ctx, bankID, ListFilter{AccountID: me, Seed: &seed1, Take: 4, Offset: 3, Limit: 20})
	if err != nil || p.Total != 4 || len(p.Items) != 1 || p.Items[0].ID != a1[3] {
		t.Fatalf("take=4 offset=3 应只剩第 4 题：%+v %v", p, err)
	}
	if p, _ := svc.ListQuestions(ctx, bankID, ListFilter{AccountID: me, Seed: &seed1, Take: 4, Offset: 4, Limit: 20}); len(p.Items) != 0 {
		t.Fatalf("take=4 offset=4 应为空，得到 %d 题", len(p.Items))
	}
	idList, err := repo.ListQuestionIDs(ctx, bankID, ListFilter{Seed: &seed1, Take: 4})
	if err != nil || !slices.Equal(idList, took) {
		t.Fatalf("ListQuestionIDs 与 ListQuestions 顺序不一致：%v / %v（%v）", idList, took, err)
	}

	// 继续的定位：按顺序的集合 = 上次那题的下一题
	s1 := "s1"
	locate := func(step string, f ListFilter, after int64, wantOffset, wantTotal int) {
		t.Helper()
		f.AccountID = me
		off, total, err := svc.LocateAfter(ctx, bankID, f, after)
		if err != nil || off != wantOffset || total != wantTotal {
			t.Fatalf("%s: 得到 (%d/%d, %v)，期望 (%d/%d)", step, off, total, err, wantOffset, wantTotal)
		}
	}
	locate("s1 做到 #2", ListFilter{Session: &s1}, q["s1#2"], 2, 3)
	locate("s1 做到最后一题", ListFilter{Session: &s1}, q["s1#3"], 3, 3)
	locate("上次那题已不在集合里", ListFilter{Session: &s1}, q["s2#1"], 0, 3)
	locate("随机一轮做到第 2 题", ListFilter{Seed: &seed1, Take: 4}, a1[1], 2, 4)

	// 小结：每题只看最近一次。⭐ 另一个账号的作答不能算进来
	attempt(t, db, me, q["s1#1"], 0)
	attempt(t, db, me, q["s1#1"], 1) // 先错后对 ⇒ 算对
	attempt(t, db, me, q["s1#2"], 0)
	attempt(t, db, other, q["s1#3"], 1)
	sum, err := svc.SummarizeSet(ctx, bankID, ListFilter{AccountID: me, Session: &s1})
	if err != nil || *sum != (SetSummary{Total: 3, Answered: 2, Correct: 1}) {
		t.Fatalf("s1 小结应为 3/2/1，得到 %+v %v", sum, err)
	}
	// 列表上的「最近一次对错」（4.3 列表的 ✓ / ✕）：s1#1 先错后对 ⇒ true，s1#2 ⇒ false，s1#3 我没做过 ⇒ nil
	// ⭐ s1#3 另一个账号做对过 —— 漏了 user_id 它就会显示成 true
	lastOf := func(who userid.UserID) []string {
		t.Helper()
		p, err := svc.ListQuestions(ctx, bankID, ListFilter{AccountID: who, Session: &s1, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, it := range p.Items {
			switch {
			case it.LastCorrect == nil:
				out = append(out, "-")
			case *it.LastCorrect:
				out = append(out, "✓")
			default:
				out = append(out, "✕")
			}
		}
		return out
	}
	if got := lastOf(me); !slices.Equal(got, []string{"✓", "✕", "-"}) {
		t.Fatalf("我的最近一次对错应为 [✓ ✕ -]，得到 %v", got)
	}
	if got := lastOf(other); !slices.Equal(got, []string{"-", "-", "✓"}) {
		t.Fatalf("另一个账号应为 [- - ✓]，得到 %v —— 串号了", got)
	}

	// 会缩短的集合：定位恒为 0（做完的题已离开集合）
	locate("未解答（会缩短）", ListFilter{Session: &s1, Mode: "unseen"}, q["s1#3"], 0, 1)
}

func entriesFixture(t *testing.T, db *sqlx.DB) (int64, map[string]int64) {
	t.Helper()
	slug := "t-ent-" + randSuffix()
	r, err := db.Exec(`INSERT INTO bank (slug,name,locale,kind) VALUES (?,?,'zh','cert')`, slug, slug)
	if err != nil {
		t.Fatal(err)
	}
	bankID, _ := r.LastInsertId()
	t.Cleanup(func() {
		// ⛔ 只删自己造的，按 bank_id 逐级清。
		for _, stmt := range []string{
			`DELETE x FROM bookmark x JOIN question q ON q.id=x.question_id WHERE q.bank_id=?`,
			`DELETE x FROM attempt x JOIN question q ON q.id=x.question_id WHERE q.bank_id=?`,
			`DELETE x FROM question_tag x JOIN question q ON q.id=x.question_id WHERE q.bank_id=?`,
			`DELETE FROM tag WHERE bank_id=?`,
			`DELETE FROM question WHERE bank_id=?`,
			`DELETE FROM bank WHERE id=?`,
		} {
			_, _ = db.Exec(stmt, bankID)
		}
	})
	q := map[string]int64{}
	for _, s := range []string{"s1", "s2"} {
		for no := 1; no <= 3; no++ {
			r, err := db.Exec(`INSERT INTO question (bank_id,external_no,session,stem,kind,pick_count)
			                   VALUES (?,?,?,'题面','single',1)`, bankID, no, s)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := r.LastInsertId()
			q[s+"#"+string(rune('0'+no))] = id
		}
	}
	return bankID, q
}

func mkTag(t *testing.T, db *sqlx.DB, bankID int64, value string, qids ...int64) int64 {
	t.Helper()
	r, err := db.Exec(`INSERT INTO tag (bank_id,type,value) VALUES (?,'topic',?)`, bankID, value)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	for _, q := range qids {
		mustExec(t, db, `INSERT INTO question_tag (question_id,tag_id) VALUES (?,?)`, q, id)
	}
	return id
}

func attempt(t *testing.T, db *sqlx.DB, who userid.UserID, q int64, correct int) {
	t.Helper()
	mustExec(t, db, `INSERT INTO attempt (user_id,question_id,chosen,correct) VALUES (?,?,'A',?)`, who, q, correct)
}

func mustExec(t *testing.T, db *sqlx.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

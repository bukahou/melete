package study

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/passgrade"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/userid"
)

// 近期正确率、整体合格等级与考纲域等级（契约 RecentAccuracy / PassGrade / TagStat.grade）。
//
// ⭐ 自造题库：⛔ 不借库里已有的题 —— CI 的测试库是空表。
func TestProgressGradeIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	svc := NewService(db, question.NewMySQLRepository(db))

	// 12 道题、答 A 为对；及格线换算 72%（安全线 82、C 档下限 57），一场考试 12 题
	b := gradeFixture(t, db, 12, `{"passScore":720,"maxScore":1000,"examQuestions":12}`)
	other := gradeFixture(t, db, 3, `{"tagTypes":{}}`) // 没有及格线；它的作答 ⛔ 不能算进 b
	me := mkUser(t, db, fmt.Sprintf("grade-%d", time.Now().UnixNano()%1e9))
	answer := func(q int64, right bool) {
		t.Helper()
		chosen := "B"
		if right {
			chosen = "A"
		}
		mustRecordAs(t, ctx, svc, me, q, chosen)
	}
	load := func(step, slug string) *Progress {
		t.Helper()
		p, err := svc.LoadProgress(ctx, me, slug)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return p
	}
	checkRecent := func(step string, p *Progress, attempts int, rate *int) {
		t.Helper()
		r := p.Recent
		if r.WindowDays != RecentWindowDays || r.AttemptCount != attempts || !sameIntPtr(r.Rate, rate) {
			t.Fatalf("%s: 近期正确率得到 {窗口 %d, 次数 %d, 正确率 %s}，期望 {%d, %d, %s}",
				step, r.WindowDays, r.AttemptCount, fmtIntPtr(r.Rate), RecentWindowDays, attempts, fmtIntPtr(rate))
		}
	}
	checkPass := func(step string, p *Progress, grade *passgrade.Grade, remaining int) {
		t.Helper()
		if p.PassGrade == nil {
			t.Fatalf("%s: 题库有及格线，PassGrade 不应缺省", step)
		}
		j := p.PassGrade
		if !sameGradePtr(j.Grade, grade) || j.Remaining != remaining {
			t.Fatalf("%s: 合格等级得到 {%s, 还差 %d}，期望 {%s, 还差 %d}",
				step, fmtGradePtr(j.Grade), j.Remaining, fmtGradePtr(grade), remaining)
		}
	}
	gA, gB := passgrade.GradeA, passgrade.GradeB

	// ① 一题没做：近期 0 次；不给等级，还差一场考试的题数；四档范围照样给
	p := load("一题没做", b.slug)
	checkRecent("一题没做", p, 0, nil)
	checkPass("一题没做", p, nil, 12)
	if bands := p.PassGrade.Bands; len(bands) != 4 || bands[0].Min != 82 || bands[1].Min != 72 || bands[2].Min != 57 {
		t.Fatalf("四档范围不对：%+v", bands)
	}

	// ② 9 次作答（前 3 道错）+ 另一个题库 3 次 ⇒ 本题库近期 9 次，不到门槛不给正确率
	for i := 0; i < 9; i++ {
		answer(b.qs[i], i >= 3)
	}
	for _, q := range other.qs {
		answer(q, true)
	}
	checkRecent("9 次", load("9 次", b.slug), 9, nil)

	// ③ 第 10 次 ⇒ 到门槛：7 / 10 = 70%
	answer(b.qs[9], true)
	checkRecent("10 次", load("10 次", b.slug), 10, intp(70))

	// ④ 窗口边界：第 1 题（错）挪到 169 小时前（窗口外），第 4 题挪到 167 小时前（窗口内）⇒ 9 次，又不到门槛
	shiftAttempt(t, db, me, b.qs[0], 169)
	shiftAttempt(t, db, me, b.qs[3], 167)
	p = load("挪出窗口", b.slug)
	checkRecent("挪出窗口", p, 9, nil)
	// 做过 10 道 < 一场考试 12 道 ⇒ 不给等级
	checkPass("做过 10 道", p, nil, 2)

	// ⑤ 再做 2 道（都对）⇒ 做满 12 道，掌握率 9 / 12 = 75% ⇒ B；近期 11 次、对 9 次 ⇒ 82%
	answer(b.qs[10], true)
	answer(b.qs[11], true)
	p = load("做满一场", b.slug)
	checkPass("做满一场", p, &gB, 0)
	checkRecent("做满一场", p, 11, intp(82))

	// ⑥ 重做第 1 题并答对：掌握率每题只看最近一次 ⇒ 10 / 12 = 83.3% ⇒ A；近期正确率每次都算 ⇒ 10 / 12 = 83%
	answer(b.qs[0], true)
	p = load("重做", b.slug)
	checkPass("重做", p, &gA, 0)
	checkRecent("重做", p, 12, intp(83))

	// ⑦ 考纲域：12 道都在的域 ⇒ 10 / 12 ⇒ A；只有 5 道的域 ⇒ 不到门槛不给；topic ⇒ 从不给
	domains, err := svc.LoadTagStats(ctx, me, b.slug, "domain", 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*passgrade.Grade{}
	for _, st := range domains {
		got[st.Value] = st.Grade
	}
	if g := got["domain-all"]; !sameGradePtr(g, &gA) {
		t.Fatalf("12 道的考纲域应为 A，得到 %s", fmtGradePtr(g))
	}
	if g, ok := got["domain-few"]; !ok || g != nil {
		t.Fatalf("5 道的考纲域应在列表里但不给等级，得到 present=%v grade=%s", ok, fmtGradePtr(g))
	}
	topics, err := svc.LoadTagStats(ctx, me, b.slug, "topic", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 || topics[0].Grade != nil {
		t.Fatalf("topic 标签不分档，得到 %+v", topics)
	}

	// ⑧ 没有及格线的题库：不给等级（整个对象缺省），考纲域也不给
	p = load("没有及格线", other.slug)
	if p.PassGrade != nil {
		t.Fatalf("没有及格线的题库不应有 PassGrade，得到 %+v", p.PassGrade)
	}
	checkRecent("没有及格线", p, 3, nil)
	otherDomains, err := svc.LoadTagStats(ctx, me, other.slug, "domain", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range otherDomains {
		if st.Grade != nil {
			t.Fatalf("没有及格线的题库，考纲域不应有等级：%+v", st)
		}
	}

	// ⑨ 合格线配置自相矛盾（安全线低于合格线）⇒ 报错，⛔ 不悄悄当作没有及格线
	if _, err := db.Exec(`UPDATE bank SET meta = ? WHERE id = ?`,
		`{"passScore":720,"maxScore":1000,"safetyRate":60}`, b.bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoadProgress(ctx, me, b.slug); !errors.Is(err, passgrade.ErrInvalidExam) {
		t.Fatalf("配置矛盾时 LoadProgress 应返回 ErrInvalidExam，得到 %v", err)
	}
	if _, err := svc.LoadTagStats(ctx, me, b.slug, "domain", 1); !errors.Is(err, passgrade.ErrInvalidExam) {
		t.Fatalf("配置矛盾时 LoadTagStats 应返回 ErrInvalidExam，得到 %v", err)
	}
}

type gradeBank struct {
	bankID int64
	slug   string
	qs     []int64
}

// gradeFixture 建一个带 meta 的题库和 n 道四选一的题（ai_verdict = A），
// 外加两个考纲域（全部题 / 前 5 题）和一个 topic（全部题）。⛔ 只删自己造的行。
func gradeFixture(t *testing.T, db *sqlx.DB, n int, meta string) gradeBank {
	t.Helper()
	slug := fmt.Sprintf("grade-%d", time.Now().UnixNano()%1e9)
	r, err := db.Exec(`INSERT INTO bank (slug,name,locale,kind,meta) VALUES (?,?,'zh','custom',?)`, slug, slug, meta)
	if err != nil {
		t.Fatalf("建题库: %v", err)
	}
	bankID, _ := r.LastInsertId()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE qt FROM question_tag qt JOIN question q ON q.id=qt.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM tag WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE c FROM card c JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE a FROM attempt a JOIN question q ON q.id=a.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE x FROM answer_claim x JOIN question q ON q.id=x.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE c FROM choice c JOIN question q ON q.id=c.question_id WHERE q.bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM question WHERE bank_id=?`, bankID)
		_, _ = db.Exec(`DELETE FROM bank WHERE id=?`, bankID)
	})

	b := gradeBank{bankID: bankID, slug: slug}
	for i := 1; i <= n; i++ {
		r, err := db.Exec(`INSERT INTO question (bank_id,external_no,stem,kind,pick_count) VALUES (?,?,?,'single',1)`,
			bankID, i, fmt.Sprintf("分档测试题 %d", i))
		if err != nil {
			t.Fatal(err)
		}
		q, _ := r.LastInsertId()
		for _, l := range []string{"A", "B", "C", "D"} {
			if _, err := db.Exec(`INSERT INTO choice (question_id,label,body) VALUES (?,?,?)`, q, l, "选项"+l); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO answer_claim (question_id,source,answer) VALUES (?,'ai_verdict','A')`, q); err != nil {
			t.Fatal(err)
		}
		b.qs = append(b.qs, q)
	}
	addTag(t, db, bankID, "domain", "domain-all", b.qs)
	addTag(t, db, bankID, "domain", "domain-few", b.qs[:min(5, n)])
	addTag(t, db, bankID, "topic", "topic-all", b.qs)
	return b
}

func addTag(t *testing.T, db *sqlx.DB, bankID int64, tagType, value string, qs []int64) {
	t.Helper()
	r, err := db.Exec(`INSERT INTO tag (bank_id,type,value) VALUES (?,?,?)`, bankID, tagType, value)
	if err != nil {
		t.Fatal(err)
	}
	tagID, _ := r.LastInsertId()
	for _, q := range qs {
		if _, err := db.Exec(`INSERT INTO question_tag (question_id,tag_id) VALUES (?,?)`, q, tagID); err != nil {
			t.Fatal(err)
		}
	}
}

// shiftAttempt 把某账号对某题的作答挪到 hoursAgo 小时之前（用数据库的时钟，与窗口的判断同一个钟）。
func shiftAttempt(t *testing.T, db *sqlx.DB, who userid.UserID, q int64, hoursAgo int) {
	t.Helper()
	if _, err := db.Exec(`UPDATE attempt SET created_at = UTC_TIMESTAMP() - INTERVAL ? HOUR
	                      WHERE user_id = ? AND question_id = ?`, hoursAgo, who, q); err != nil {
		t.Fatal(err)
	}
}

func intp(v int) *int { return &v }

func sameIntPtr(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func sameGradePtr(a, b *passgrade.Grade) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func fmtIntPtr(p *int) string {
	if p == nil {
		return "缺省"
	}
	return fmt.Sprint(*p)
}

func fmtGradePtr(p *passgrade.Grade) string {
	if p == nil {
		return "缺省"
	}
	return string(*p)
}

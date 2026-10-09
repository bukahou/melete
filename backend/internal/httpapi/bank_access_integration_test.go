package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/access"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/auth"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/glossary"
	"github.com/bukahou/melete/backend/internal/httpauth"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/study"
	"github.com/bukahou/melete/backend/internal/userid"
)

// 题库权限在接口层的全覆盖（P9 #27）：普通用户对私有题库的【每一个】接口都必须是 404，且什么都不写进库。
// 高级用户对同一批接口都能拿到内容。
//
// ⭐ 为什么逐个接口测：设计文档 §4.1 那张表里，最容易漏的是路径里没有题库名的接口
// （按题目 / 术语 / 作答的编号）—— 漏一个就等于知道编号就能绕过。
func TestBankAccessIntegration(t *testing.T) {
	dsn := os.Getenv("MELETE_TEST_DSN")
	if dsn == "" {
		t.Skip("未设 MELETE_TEST_DSN，跳过集成测试")
	}
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := r.LastInsertId()
		return id
	}

	// ---- 夹具：一个私有题库（含题目 / 标签 / 术语）、一个普通用户、一个高级用户 ----
	slug := fmt.Sprintf("t-priv-%d", time.Now().UnixNano()%1e9)
	bankID := exec(`INSERT INTO bank (slug, name, locale, kind, visibility) VALUES (?, ?, 'zh', 'cert', 'private')`, slug, slug)
	qid := exec(`INSERT INTO question (bank_id, external_no, stem, kind, pick_count) VALUES (?, 1, '私有题干', 'single', 1)`, bankID)
	exec(`INSERT INTO choice (question_id, label, body) VALUES (?, 'A', '甲'), (?, 'B', '乙')`, qid, qid)
	termID := exec(`INSERT INTO term (bank_id, slug, names, definition, category) VALUES (?, 'Private term', '{"zh":"私有术语"}', '{"zh":"释义"}', 'X')`, bankID)
	newUser := func(name string, perms ...string) userid.UserID {
		t.Helper()
		raw, _ := userid.New()
		id := userid.UserID(raw)
		exec(`INSERT INTO users (id, username, status, created_at, updated_at) VALUES (?, ?, 1, UTC_TIMESTAMP(), UTC_TIMESTAMP())`,
			id, fmt.Sprintf("t-ba-%s-%d", name, time.Now().UnixNano()%1e9))
		for _, p := range perms {
			exec(`INSERT INTO user_permissions (user_id, permission, granted_at) VALUES (?, ?, UTC_TIMESTAMP())`, id, p)
		}
		return id
	}
	basic, adv := newUser("basic"), newUser("adv", access.PermPrivate)
	t.Cleanup(func() {
		for _, id := range []userid.UserID{basic, adv} {
			_, _ = db.Exec(`DELETE FROM attempt WHERE user_id = ?`, id)
			_, _ = db.Exec(`DELETE FROM card WHERE user_id = ?`, id)
			_, _ = db.Exec(`DELETE FROM bookmark WHERE user_id = ?`, id)
			_, _ = db.Exec(`DELETE FROM user_permissions WHERE user_id = ?`, id)
			_, _ = db.Exec(`DELETE FROM users WHERE id = ?`, id)
		}
		_, _ = db.Exec(`DELETE FROM term WHERE id = ?`, termID)
		_, _ = db.Exec(`DELETE FROM choice WHERE question_id = ?`, qid)
		_, _ = db.Exec(`DELETE FROM question WHERE id = ?`, qid)
		_, _ = db.Exec(`DELETE FROM bank WHERE id = ?`, bankID)
	})

	qrepo := question.NewMySQLRepository(db)
	srv := NewServer(bank.NewService(bank.NewMySQLRepository(db)), question.NewService(qrepo),
		study.NewService(db, qrepo), glossary.NewService(db), access.NewService(db),
		nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// 带身份的上下文：走真实的认证中间件 + 假的令牌解析器 —— ⛔ 不为测试开「直接注入身份」的后门
	as := func(id userid.UserID) context.Context {
		var got context.Context
		h := httpauth.RequireUserExcept(fakeParser{id: string(id)})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = r.Context()
		}))
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer test")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got == nil {
			t.Fatal("中间件没有放行")
		}
		return got
	}

	sp := &slug
	var attemptID int64
	type probe struct {
		name string
		call func(ctx context.Context) (any, error)
	}
	probes := []probe{
		{"GET /banks/{slug}", func(c context.Context) (any, error) { return srv.GetBank(c, api.GetBankRequestObject{Slug: slug}) }},
		{"GET /banks/{slug}/tags", func(c context.Context) (any, error) {
			return srv.ListBankTags(c, api.ListBankTagsRequestObject{Slug: slug})
		}},
		{"GET /banks/{slug}/questions", func(c context.Context) (any, error) {
			return srv.ListQuestions(c, api.ListQuestionsRequestObject{Slug: slug})
		}},
		{"GET /banks/{slug}/questions/summary", func(c context.Context) (any, error) {
			return srv.SummarizeQuestions(c, api.SummarizeQuestionsRequestObject{Slug: slug})
		}},
		{"GET /banks/{slug}/terms", func(c context.Context) (any, error) { return srv.ListTerms(c, api.ListTermsRequestObject{Slug: slug}) }},
		{"GET /terms/{id}", func(c context.Context) (any, error) { return srv.GetTerm(c, api.GetTermRequestObject{Id: termID}) }},
		{"GET /questions/{id}", func(c context.Context) (any, error) { return srv.GetQuestion(c, api.GetQuestionRequestObject{Id: qid}) }},
		{"PUT /me/bookmarks/{id}", func(c context.Context) (any, error) {
			return srv.AddBookmark(c, api.AddBookmarkRequestObject{QuestionId: qid})
		}},
		{"DELETE /me/bookmarks/{id}", func(c context.Context) (any, error) {
			return srv.RemoveBookmark(c, api.RemoveBookmarkRequestObject{QuestionId: qid})
		}},
		{"POST /attempts", func(c context.Context) (any, error) {
			return srv.RecordAttempt(c, api.RecordAttemptRequestObject{Body: &api.RecordAttemptJSONRequestBody{QuestionId: qid, Chosen: "A"}})
		}},
		{"GET /me/progress?bank=", func(c context.Context) (any, error) {
			return srv.GetMyProgress(c, api.GetMyProgressRequestObject{Params: api.GetMyProgressParams{Bank: sp}})
		}},
		{"GET /me/tag-stats?bank=", func(c context.Context) (any, error) {
			return srv.GetMyTagStats(c, api.GetMyTagStatsRequestObject{Params: api.GetMyTagStatsParams{Bank: sp, Type: api.GetMyTagStatsParamsTypeDomain}})
		}},
		{"GET /me/resume?bank=", func(c context.Context) (any, error) {
			return srv.GetMyResume(c, api.GetMyResumeRequestObject{Params: api.GetMyResumeParams{Bank: sp}})
		}},
		{"PUT /me/bank", func(c context.Context) (any, error) {
			return srv.ChooseMyBank(c, api.ChooseMyBankRequestObject{Body: &api.ChooseMyBankJSONRequestBody{BankSlug: slug}})
		}},
	}
	is404 := func(resp any) bool {
		switch resp.(type) {
		case api.GetBank404JSONResponse, api.ListBankTags404JSONResponse, api.ListQuestions404JSONResponse,
			api.SummarizeQuestions404JSONResponse, api.ListTerms404JSONResponse, api.GetTerm404JSONResponse,
			api.GetQuestion404JSONResponse, api.AddBookmark404JSONResponse, api.RemoveBookmark404JSONResponse,
			api.RecordAttempt404JSONResponse, api.RateAttempt404JSONResponse, api.GetMyProgress404JSONResponse,
			api.GetMyTagStats404JSONResponse, api.GetMyResume404JSONResponse, api.ChooseMyBank404JSONResponse:
			return true
		}
		return false
	}

	// ① 高级用户：每一个都拿得到（顺带留下一条作答，给 ③ 的 PATCH /attempts 用）
	advCtx := as(adv)
	for _, p := range probes {
		resp, err := p.call(advCtx)
		if err != nil {
			t.Fatalf("高级用户 %s 出错：%v", p.name, err)
		}
		if is404(resp) {
			t.Fatalf("高级用户 %s 不应是 404", p.name)
		}
		if r, ok := resp.(api.RecordAttempt200JSONResponse); ok && r.AttemptId != nil {
			attemptID = *r.AttemptId
		}
	}
	if banks, _ := srv.ListBanks(advCtx, api.ListBanksRequestObject{}); !containsBank(banks, slug) {
		t.Fatal("高级用户的题库列表里应有私有题库")
	}

	// ② 普通用户：每一个都是 404
	basicCtx := as(basic)
	for _, p := range probes {
		resp, err := p.call(basicCtx)
		if err != nil {
			t.Fatalf("普通用户 %s 出错（应为 404 而不是错误）：%v", p.name, err)
		}
		if !is404(resp) {
			t.Fatalf("普通用户 %s 应为 404，得到 %T —— 私有题库漏了", p.name, resp)
		}
	}
	if banks, _ := srv.ListBanks(basicCtx, api.ListBanksRequestObject{}); containsBank(banks, slug) {
		t.Fatal("普通用户的题库列表里不应有私有题库")
	}
	// 什么都没写进库
	var n int
	if err := db.Get(&n, `SELECT (SELECT COUNT(*) FROM attempt WHERE user_id = ?) + (SELECT COUNT(*) FROM bookmark WHERE user_id = ?)`, basic, basic); err != nil || n != 0 {
		t.Fatalf("普通用户不应留下作答或收藏，得到 %d 条（%v）", n, err)
	}

	// ③ 作答记录：高级用户自己的作答，被降级后连补评分都是 404（记录本身保留）
	if attemptID == 0 {
		t.Fatal("高级用户应已留下一条作答")
	}
	if _, err := db.Exec(`DELETE FROM user_permissions WHERE user_id = ?`, adv); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.RateAttempt(as(adv), api.RateAttemptRequestObject{Id: attemptID, Body: &api.RateAttemptJSONRequestBody{Rating: 3}})
	if err != nil || !is404(resp) {
		t.Fatalf("降级后 PATCH /attempts/{id} 应为 404，得到 %T（%v）", resp, err)
	}
	if err := db.Get(&n, `SELECT COUNT(*) FROM attempt WHERE id = ?`, attemptID); err != nil || n != 1 {
		t.Fatalf("降级后作答记录应保留，得到 %d（%v）", n, err)
	}
}

type fakeParser struct{ id string }

func (f fakeParser) ParseAccessToken(string) (auth.Claims, error) {
	return auth.Claims{UserID: f.id, SessionID: "test-session"}, nil
}

func containsBank(resp api.ListBanksResponseObject, slug string) bool {
	list, ok := resp.(api.ListBanks200JSONResponse)
	if !ok {
		return false
	}
	for _, b := range list {
		if b.Slug == slug {
			return true
		}
	}
	return false
}

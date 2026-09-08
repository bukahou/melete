package question

import (
	"context"
	"errors"
)

// ErrModeNeedsAccount 表示个人化模式缺少账号标识。
var ErrModeNeedsAccount = errors.New("wrong/unsure/unseen 模式需要账号")

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Service 是题目领域对外的门面。
// 上层（HTTP handler）只依赖这个接口，不依赖 Repository 或具体 SQL。
type Service interface {
	ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error)
	// GetQuestion 的 locale 空串 = 源语言。译文缺失时回退源语言，
	// 并由 Detail 上的 Localized 告诉界面「这是回退」（⛔ 不静默）。
	GetQuestion(ctx context.Context, id int64, locale string) (*Detail, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

// ListQuestions 在进仓储之前把分页参数收敛到合法范围，
// 避免 limit=100000 这类请求打穿数据库。
func (s *service) ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error) {
	// ⚠️ 空串 = 未认证。⛔ 与旧的 `<= 0` 同一条纪律：零值不是合法身份。
	if f.Mode != "" && f.AccountID == "" {
		return nil, ErrModeNeedsAccount
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return s.repo.ListQuestions(ctx, bankID, f)
}

func (s *service) GetQuestion(ctx context.Context, id int64, locale string) (*Detail, error) {
	return s.repo.FindQuestionByID(ctx, id, locale)
}

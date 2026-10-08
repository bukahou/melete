package question

import (
	"context"
	"errors"
)

// ErrModeNeedsAccount 表示个人化模式缺少账号标识。
var ErrModeNeedsAccount = errors.New("个人化条件（mode / 收藏 / 小结）需要账号")

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
	// LocateAfter 返回集合里「questionID 的下一题」的 offset 与集合大小（P9 #13 继续）。
	// 会缩短的集合（wrong / unsure / unseen / due）恒为 0 —— 做完的题已离开集合，队首就是下一题。
	// questionID 已不在集合里（标签被重跑富化改掉等）也回 0：⛔ 不猜一个位置。
	LocateAfter(ctx context.Context, bankID int64, f ListFilter, questionID int64) (offset, total int, err error)
	// SummarizeSet 是集合的本轮小结：多大、做过几道、最近一次答对几道（P9 #19）。
	SummarizeSet(ctx context.Context, bankID int64, f ListFilter) (*SetSummary, error)
}

// SetSummary 见 SummarizeSet。
type SetSummary struct {
	Total, Answered, Correct int
}

// shrinkingModes 的集合会随作答缩短。
var shrinkingModes = map[string]bool{"wrong": true, "unsure": true, "unseen": true, "due": true}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

// ListQuestions 在进仓储之前把分页参数收敛到合法范围，
// 避免 limit=100000 这类请求打穿数据库。
func (s *service) ListQuestions(ctx context.Context, bankID int64, f ListFilter) (*Page, error) {
	if err := needsAccount(f); err != nil {
		return nil, err
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

// needsAccount 个人化条件必须有账号。⚠️ 空串 = 未认证 —— 零值不是合法身份。
func needsAccount(f ListFilter) error {
	if (f.Mode != "" || f.OnlyBookmarked) && f.AccountID == "" {
		return ErrModeNeedsAccount
	}
	return nil
}

func (s *service) LocateAfter(ctx context.Context, bankID int64, f ListFilter, questionID int64) (int, int, error) {
	if err := needsAccount(f); err != nil {
		return 0, 0, err
	}
	ids, err := s.repo.ListQuestionIDs(ctx, bankID, f)
	if err != nil {
		return 0, 0, err
	}
	if shrinkingModes[f.Mode] {
		return 0, len(ids), nil
	}
	for i, id := range ids {
		if id == questionID {
			return i + 1, len(ids), nil
		}
	}
	return 0, len(ids), nil
}

func (s *service) SummarizeSet(ctx context.Context, bankID int64, f ListFilter) (*SetSummary, error) {
	if f.AccountID == "" {
		return nil, ErrModeNeedsAccount
	}
	ids, err := s.repo.ListQuestionIDs(ctx, bankID, f)
	if err != nil {
		return nil, err
	}
	answered, correct, err := s.repo.SummarizeAnswers(ctx, f.AccountID, ids)
	if err != nil {
		return nil, err
	}
	return &SetSummary{Total: len(ids), Answered: answered, Correct: correct}, nil
}

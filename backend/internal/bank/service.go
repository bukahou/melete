package bank

import "context"

// Service 是题库领域对外的门面。
// 上层（HTTP handler）只依赖这个接口，不依赖 Repository 或具体 SQL。
type Service interface {
	ListBanks(ctx context.Context) ([]Bank, error)
	GetBankDetail(ctx context.Context, slug string) (*Bank, *Stats, error)
	ListTags(ctx context.Context, slug, tagType string) ([]Tag, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

func (s *service) ListBanks(ctx context.Context) ([]Bank, error) {
	return s.repo.ListBanks(ctx)
}

func (s *service) GetBankDetail(ctx context.Context, slug string) (*Bank, *Stats, error) {
	b, err := s.repo.FindBankBySlug(ctx, slug)
	if err != nil {
		return nil, nil, err
	}
	stats, err := s.repo.LoadStats(ctx, b.ID)
	if err != nil {
		return nil, nil, err
	}
	return b, stats, nil
}

func (s *service) ListTags(ctx context.Context, slug, tagType string) ([]Tag, error) {
	b, err := s.repo.FindBankBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.repo.ListTags(ctx, b.ID, tagType)
}

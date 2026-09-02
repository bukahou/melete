package account

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// ErrBadCredentials 统一「用户不存在」与「密码错误」——
// 登录失败不区分原因，避免用户名枚举。
var ErrBadCredentials = errors.New("用户名或密码错误")

// Service 是账号领域对外的门面。
type Service interface {
	VerifyPassword(ctx context.Context, username, password string) (*Account, error)
	EstablishSSO(ctx context.Context, sub, display string) (*Account, error)
	FindByID(ctx context.Context, id int64) (*Account, error)
}

type service struct{ repo Repository }

func NewService(repo Repository) Service { return &service{repo: repo} }

func (s *service) VerifyPassword(ctx context.Context, username, password string) (*Account, error) {
	a, err := s.repo.FindByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		// 仍然烧一次 bcrypt，让两种失败耗时一致（防时序侧信道的用户名枚举）
		_ = bcrypt.CompareHashAndPassword(
			[]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"), []byte(password))
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	if a.PasswordHash == nil ||
		bcrypt.CompareHashAndPassword([]byte(*a.PasswordHash), []byte(password)) != nil {
		return nil, ErrBadCredentials
	}
	return a, nil
}

func (s *service) EstablishSSO(ctx context.Context, sub, display string) (*Account, error) {
	return s.repo.UpsertByAkashaSub(ctx, sub, display)
}

func (s *service) FindByID(ctx context.Context, id int64) (*Account, error) {
	return s.repo.FindByID(ctx, id)
}

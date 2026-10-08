package httpapi

import (
	"context"
	"errors"

	"github.com/bukahou/melete/backend/internal/access"
	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/bank"
	"github.com/bukahou/melete/backend/internal/userid"
)

// 题库权限在接口层的落点（P9 #27–#30，设计见 docs/design/active/bank-access.md）。
//
// ⭐ 每个和题库有关的端点第一步都是 viewer：谁在看、能看哪些题库。之后只用它返回的 Scope 判断。
// ⭐ 看不到 = 不存在：一律回 404（与「真的不存在」同一句话），⛔ 不回 403 —— 普通用户连「有这个题库」都无从得知。

// viewer 取当前账号与它的 Scope。读权限失败 ⇒ 返回错误（500），⛔ 不降级成「普通用户」继续。
func (s *Server) viewer(ctx context.Context, op string) (userid.UserID, access.Scope, error) {
	id, err := s.requireAccount(ctx, op)
	if err != nil {
		return "", access.Scope{}, err
	}
	_, scope, err := s.access.Resolve(ctx, id)
	if err != nil {
		return "", access.Scope{}, s.fail(op+".access", err)
	}
	return id, scope, nil
}

// visibleBank 按 slug 取题库；不存在或看不到都返回 bank.ErrNotFound。
func (s *Server) visibleBank(ctx context.Context, scope access.Scope, slug string) (*bank.Bank, error) {
	b, err := s.banks.FindBank(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !scope.Allows(b.Visibility) {
		return nil, bank.ErrNotFound
	}
	return b, nil
}

// questionVisible / attemptVisible：路径里没有题库名的接口用。对象不存在也返回 false —— 两者对外是同一个 404。
func (s *Server) questionVisible(ctx context.Context, scope access.Scope, questionID int64) (bool, error) {
	v, err := s.access.QuestionVisibility(ctx, questionID)
	if errors.Is(err, access.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return scope.Allows(v), nil
}

func (s *Server) attemptVisible(ctx context.Context, scope access.Scope, attemptID int64) (bool, error) {
	v, err := s.access.AttemptVisibility(ctx, attemptID)
	if errors.Is(err, access.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return scope.Allows(v), nil
}

// GetMyAccess 我的档位。界面据此显示 / 隐藏管理入口 —— 那只是方便，门在各接口自己。
func (s *Server) GetMyAccess(ctx context.Context, _ api.GetMyAccessRequestObject) (api.GetMyAccessResponseObject, error) {
	id, err := s.requireAccount(ctx, "GetMyAccess")
	if err != nil {
		return nil, err
	}
	tier, _, err := s.access.Resolve(ctx, id)
	if err != nil {
		return nil, s.fail("GetMyAccess", err)
	}
	return api.GetMyAccess200JSONResponse{Tier: api.Tier(tier)}, nil
}

// ListAdminUsers 用户列表。非 admin ⇒ 404（⛔ 不暴露「有这个接口」）。
func (s *Server) ListAdminUsers(ctx context.Context, req api.ListAdminUsersRequestObject) (api.ListAdminUsersResponseObject, error) {
	id, err := s.requireAccount(ctx, "ListAdminUsers")
	if err != nil {
		return nil, err
	}
	page, size := deref(req.Params.Page), deref(req.Params.PageSize)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 50
	}
	users, total, err := s.access.ListUsers(ctx, id, page, size)
	if errors.Is(err, access.ErrNotAdmin) {
		return api.ListAdminUsers404JSONResponse{NotFoundJSONResponse: notFound("不存在")}, nil
	}
	if err != nil {
		return nil, s.fail("ListAdminUsers", err)
	}
	out := api.ListAdminUsers200JSONResponse{Items: make([]api.AdminUser, 0, len(users)), Total: total, Page: page, PageSize: size}
	for _, u := range users {
		item := api.AdminUser{
			Id: string(u.ID), DisplayName: u.DisplayName, Tier: api.Tier(u.Tier),
			CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt,
		}
		if u.Email != "" {
			email := u.Email
			item.Email = &email
		}
		if u.AvatarURL != "" {
			avatar := u.AvatarURL
			item.AvatarUrl = &avatar
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// SetUserTier 升级 / 降级。admin 不能操作任何 admin（含自己）⇒ 409。每次操作都进日志：谁、对谁、设成什么。
func (s *Server) SetUserTier(ctx context.Context, req api.SetUserTierRequestObject) (api.SetUserTierResponseObject, error) {
	id, err := s.requireAccount(ctx, "SetUserTier")
	if err != nil {
		return nil, err
	}
	if req.Body == nil || !req.Body.Tier.Valid() {
		return api.SetUserTier404JSONResponse{NotFoundJSONResponse: notFound("不存在")}, nil
	}
	// ⚠️ 路径里的 userId 必须是规范的账号 id 文本 —— 不规范的一律当作不存在（⛔ 不尝试「修正」它）
	if _, err := userid.Encode(req.UserId); err != nil {
		return api.SetUserTier404JSONResponse{NotFoundJSONResponse: notFound("用户不存在")}, nil
	}
	target := userid.UserID(req.UserId)
	tier := access.Tier(req.Body.Tier)
	err = s.access.SetTier(ctx, id, target, tier)
	switch {
	case errors.Is(err, access.ErrNotAdmin), errors.Is(err, access.ErrNotFound), errors.Is(err, access.ErrInvalidTier):
		return api.SetUserTier404JSONResponse{NotFoundJSONResponse: notFound("不存在")}, nil
	case errors.Is(err, access.ErrTargetIsAdmin):
		return api.SetUserTier409JSONResponse{Message: err.Error()}, nil
	case err != nil:
		return nil, s.fail("SetUserTier", err)
	}
	s.log.Info("用户档位已变更", "actor", string(id), "target", string(target), "tier", string(tier))
	return api.SetUserTier204Response{}, nil
}

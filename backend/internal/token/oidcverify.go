package token

import (
	"context"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/bukahou/melete/backend/internal/account"
)

// OIDCVerifier 验证 Akasha 签发的 id_token。
//
// **这是 iOS 端能存在的前提**：原生 App 自己走完 OIDC 流程后把 id_token 交过来，
// 本 API 必须自己验签才能确认「你是谁」——绝不能接受客户端自称的 sub，
// 那等于任何人都能登录任意账号（本 API 公网暴露前的旧实现正是如此，已修）。
type OIDCVerifier struct {
	issuer   string
	clientID string

	// discovery 要联网，且 Akasha 可能暂时不可达 —— 延迟到首次使用再初始化，
	// 失败也不缓存，下次重试。若在 main 里初始化，Akasha 抖一下 api 就起不来。
	once     sync.Once
	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

func NewOIDCVerifier(issuer, clientID string) *OIDCVerifier {
	return &OIDCVerifier{issuer: issuer, clientID: clientID}
}

// Verify 验签并返回上游带来的资料（sub · 展示名 · 邮箱 · 头像）。
// ⚠️ 2026-10-08 之前只返回 (sub, 展示名)，邮箱与头像在这里就被丢掉了 —— 管理页因此认不出人。
func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (account.FederatedProfile, error) {
	ver, err := v.get(ctx)
	if err != nil {
		return account.FederatedProfile{}, err
	}
	// go-oidc 校验签名、issuer、audience、exp —— 缺一不可
	idToken, err := ver.Verify(ctx, rawIDToken)
	if err != nil {
		return account.FederatedProfile{}, fmt.Errorf("id_token 验签失败: %w", err)
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		Picture           string `json:"picture"`
	}
	_ = idToken.Claims(&claims) // 资料缺失不该让登录失败
	return account.FederatedProfile{
		Subject:   idToken.Subject,
		Display:   DisplayName(claims.Name, claims.PreferredUsername),
		Email:     claims.Email,
		AvatarURL: claims.Picture,
	}, nil
}

// DisplayName 展示名的取法：name → preferred_username；都没有返回空串。web（oidcrp）与 iOS（id_token）两条路共用。
// ⚠️ 不在这里兜底成「学习者」：登录时要拿它同步老账号，兜底值会把原来的名字覆盖掉。兜底只在建号时做。
func DisplayName(name, preferred string) string {
	if name != "" {
		return name
	}
	return preferred
}

func (v *OIDCVerifier) get(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	provider, err := oidc.NewProvider(ctx, v.issuer)
	if err != nil {
		return nil, fmt.Errorf("连接 OIDC 提供方 %s: %w", v.issuer, err)
	}
	v.verifier = provider.Verifier(&oidc.Config{ClientID: v.clientID})
	return v.verifier, nil
}

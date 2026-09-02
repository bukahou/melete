package token

import (
	"context"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
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

// Verify 验签并返回 (sub, 展示名)。
func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (string, string, error) {
	ver, err := v.get(ctx)
	if err != nil {
		return "", "", err
	}
	// go-oidc 校验签名、issuer、audience、exp —— 缺一不可
	idToken, err := ver.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", fmt.Errorf("id_token 验签失败: %w", err)
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	_ = idToken.Claims(&claims) // 展示名缺失不该让登录失败

	display := claims.Name
	if display == "" {
		display = claims.PreferredUsername
	}
	if display == "" {
		display = "学习者"
	}
	return idToken.Subject, display, nil
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

// Package token 负责 access / refresh 双 token 的签发与校验。
//
// 形态对齐 geass-v3（见其 internal/auth/DECISIONS.md）：
//   access  —— HS256 JWT，TTL 1h，**不落库**。验签不查库，撤回的代价由短 TTL 兜住
//   refresh —— 不透明随机串，**落库**（account_session）。生命周期以月计的凭证
//              必须能撤回：登出、换设备、密码泄漏。吊销 = is_valid 置 0
//
// 库里只存 refresh 的 SHA-256：数据库被读走也无法直接冒用。
package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
)

const (
	// AccessTTL 对齐 geass-v3 的 1 小时
	AccessTTL = time.Hour
	// RefreshTTL 30 天：够长到 iOS 用户不必频繁重登，短到丢失的凭证会自然失效
	RefreshTTL = 30 * 24 * time.Hour
	issuer     = "melete-api"
)

var (
	ErrInvalidToken = errors.New("token 无效")
	ErrRevoked      = errors.New("会话已失效")
)

// Pair 是一次签发的结果。
type Pair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// Issuer 签发并校验 token。
type Issuer struct {
	db     *sqlx.DB
	secret []byte
}

func NewIssuer(db *sqlx.DB, secret string) *Issuer {
	return &Issuer{db: db, secret: []byte(secret)}
}

// Issue 为账号建立一个新会话，返回双 token。
func (i *Issuer) Issue(ctx context.Context, accountID int64, deviceInfo string) (*Pair, error) {
	access, err := i.signAccess(accountID)
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	_, err = i.db.ExecContext(ctx, `
		INSERT INTO account_session (account_id, token_hash, device_info, expires_at)
		VALUES (?, ?, ?, ?)`,
		accountID, hashToken(refresh), nullIfEmpty(deviceInfo), time.Now().Add(RefreshTTL))
	if err != nil {
		return nil, fmt.Errorf("建立会话: %w", err)
	}
	return &Pair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(AccessTTL.Seconds())}, nil
}

// Refresh 用 refresh token 换一对新的，并**轮换 refresh 本身**。
//
// 轮换（rotation）是有意的：一个 refresh 只能用一次，用过即失效。
// 若旧 refresh 被窃取后再次使用，它已经不在库里 —— 攻击暴露，而不是静默共存。
func (i *Issuer) Refresh(ctx context.Context, refresh string) (*Pair, int64, error) {
	var row struct {
		ID        int64 `db:"id"`
		AccountID int64 `db:"account_id"`
	}
	err := i.db.GetContext(ctx, &row, `
		SELECT id, account_id FROM account_session
		WHERE token_hash = ? AND is_valid = 1 AND expires_at > NOW()`, hashToken(refresh))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrRevoked
	}
	if err != nil {
		return nil, 0, fmt.Errorf("查询会话: %w", err)
	}

	access, err := i.signAccess(row.AccountID)
	if err != nil {
		return nil, 0, err
	}
	next, err := randomToken()
	if err != nil {
		return nil, 0, err
	}
	// 就地轮换：同一行换掉 hash 并顺延有效期，会话历史（device_info/created_at）得以保留
	res, err := i.db.ExecContext(ctx, `
		UPDATE account_session SET token_hash = ?, expires_at = ?
		WHERE id = ? AND is_valid = 1`, hashToken(next), time.Now().Add(RefreshTTL), row.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("轮换会话: %w", err)
	}
	// 并发刷新时只有一方能改到这一行，另一方拿到 0 行 —— 视为已失效，避免两份有效 refresh
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, 0, ErrRevoked
	}
	return &Pair{AccessToken: access, RefreshToken: next, ExpiresIn: int(AccessTTL.Seconds())}, row.AccountID, nil
}

// Revoke 吊销会话。幂等：token 不存在也返回 nil。
func (i *Issuer) Revoke(ctx context.Context, refresh string) error {
	_, err := i.db.ExecContext(ctx,
		`UPDATE account_session SET is_valid = 0 WHERE token_hash = ?`, hashToken(refresh))
	return err
}

// ParseAccessToken 验签并取出 accountID。
// 显式限定 HS256：否则可用 alg=none 或换成非对称算法绕过验签。
func (i *Issuer) ParseAccessToken(raw string) (int64, error) {
	t, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired())
	if err != nil {
		return 0, ErrInvalidToken
	}
	sub, err := t.Claims.GetSubject()
	if err != nil {
		return 0, ErrInvalidToken
	}
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidToken
	}
	return id, nil
}

func (i *Issuer) signAccess(accountID int64) (string, error) {
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   strconv.FormatInt(accountID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
	}).SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("签发 access token: %w", err)
	}
	return s, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机 token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

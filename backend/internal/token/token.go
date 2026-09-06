// Package token 负责 access / refresh 双 token 的签发与校验。
//
//	access  —— HS256 JWT，TTL 1h，**不落库**。验签不查库，撤回的代价由短 TTL 兜住
//	refresh —— 不透明随机串，**落库**（user_sessions）。生命周期以月计的凭证
//	           必须能撤回：登出、换设备、密码泄漏
//
// 库里只存 refresh 的 SHA-256：数据库被读走也无法直接冒用。
//
// ⚠️ 2026-09-07（localauth 阶段 2）两处变更：
//  1. accountID 从 int64 换成 canonical UUID 文本（案卷 §22.2）
//  2. 会话从 account_session 换到 user_sessions —— 后者是 localauth 接入后
//     要长期用的表，⛔ 不给旧表加 UUID 列做一次性桥
//
// ⚠️ 本包在阶段 3 会被 localauth 的 SessionGuard 整体取代。留着它到那时，
// 是因为 melete 已上线、用户每天在用 —— 先删后建会让中间过程登录不可用。
package token

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

const (
	// AccessTTL ⚠️ 案卷裁决 ② 定为 15min，但那一项与 localauth 的
	// SessionGuard 一起在阶段 3 落地（work 2026-09-06）。在此之前保持 1h ——
	// 提前改只会让下方那个竞态的触发频率 ×4 而没有对应的防护。
	AccessTTL = time.Hour
	// RefreshTTL 30 天
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
func (i *Issuer) Issue(ctx context.Context, userID string, deviceInfo string) (*Pair, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return nil, err
	}
	access, err := i.signAccess(userID)
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	sid, err := userid.New()
	if err != nil {
		return nil, fmt.Errorf("生成会话 id: %w", err)
	}
	sidBin, err := userid.Encode(sid)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = i.db.ExecContext(ctx, `
		INSERT INTO user_sessions
		  (id, user_id, refresh_hash, device_info, created_at, last_active_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sidBin, uid, hashToken(refresh), nullIfEmpty(deviceInfo), now, now, now.Add(RefreshTTL))
	if err != nil {
		return nil, fmt.Errorf("建立会话: %w", err)
	}
	return &Pair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(AccessTTL.Seconds())}, nil
}

// Refresh 用 refresh token 换一对新的，并**轮换 refresh 本身**。
//
// 轮换是有意的：一个 refresh 只能用一次。旧 refresh 被窃取后再次使用时，
// 它已经不在库里 —— 攻击暴露，而不是静默共存。
//
// ⛔⛔ UPDATE 必须按【旧哈希】匹配，⛔ 不能按行 id。
//
// ⚠️ 2026-09-07 修复的真实竞态：这里曾经是
//
//	UPDATE ... SET token_hash = ? WHERE id = ? AND is_valid = 1
//
// 并且注释断言「并发时另一方拿到 0 行」。dev 库实测证伪 ——
// 两个并发 UPDATE 都影响 1 行，于是两个请求各拿到一张新 refresh、
// 库里只留后者，先拿到的那张是**死票**，下次刷新被踢回登录页。
// 触发路径是 Next.js 的 <Link> 预取：access cookie 过期后一次渲染会
// 并发发起约 10 次刷新。
//
// ⭐ 这个缺陷同时被 gokit 的契约套件逮住：把本函数改回按 id 匹配再跑
// RunSessionStoreTests，报「并发 32 次 Rotate 同一旧哈希成功了 5 次」。
// 按旧哈希匹配之后第二个 UPDATE 匹配 0 行，竞态消失。
func (i *Issuer) Refresh(ctx context.Context, refresh string) (*Pair, string, error) {
	oldHash := hashToken(refresh)
	var row struct {
		UserID []byte `db:"user_id"`
	}
	err := i.db.GetContext(ctx, &row, `
		SELECT user_id FROM user_sessions
		 WHERE refresh_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
		oldHash, time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrRevoked
	}
	if err != nil {
		return nil, "", fmt.Errorf("查询会话: %w", err)
	}
	userID, err := userid.Decode(row.UserID)
	if err != nil {
		return nil, "", err
	}

	access, err := i.signAccess(userID)
	if err != nil {
		return nil, "", err
	}
	next, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	res, err := i.db.ExecContext(ctx, `
		UPDATE user_sessions
		   SET prev_refresh_hash = refresh_hash, refresh_hash = ?,
		       expires_at = ?, last_active_at = ?
		 WHERE refresh_hash = ? AND revoked_at IS NULL`,
		hashToken(next), now.Add(RefreshTTL), now, oldHash)
	if err != nil {
		return nil, "", fmt.Errorf("轮换会话: %w", err)
	}
	// ⭐ 条件在 WHERE 里，胜负由受影响行数决定 —— 并发的另一方匹配 0 行。
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, "", ErrRevoked
	}
	return &Pair{AccessToken: access, RefreshToken: next, ExpiresIn: int(AccessTTL.Seconds())}, userID, nil
}

// Revoke 吊销会话。幂等：token 不存在也返回 nil。
func (i *Issuer) Revoke(ctx context.Context, refresh string) error {
	_, err := i.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = ? WHERE refresh_hash = ? AND revoked_at IS NULL`,
		time.Now().UTC(), hashToken(refresh))
	return err
}

// ParseAccessToken 验签并取出 userID。
// 显式限定 HS256：否则可用 alg=none 或换成非对称算法绕过验签。
func (i *Issuer) ParseAccessToken(raw string) (string, error) {
	t, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired())
	if err != nil {
		return "", ErrInvalidToken
	}
	sub, err := t.Claims.GetSubject()
	if err != nil {
		return "", ErrInvalidToken
	}
	// ⭐ 在这里就把形态校严：sub 必须是 canonical UUID。
	// ⚠️ 少了这一步，一个畸形 sub 会一路传到 SQL 层才炸，
	// 而那时错误信息里已经没有「这是个 token 问题」的线索了。
	if _, err := userid.Encode(sub); err != nil {
		return "", ErrInvalidToken
	}
	return sub, nil
}

func (i *Issuer) signAccess(userID string) (string, error) {
	now := time.Now()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID,
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

// hashToken ⚠️ 返回 []byte 而不是 hex 文本 —— user_sessions.refresh_hash
// 是 BINARY(32)，与 localauthx.sessionStore 用同一种形态，
// ⛔ 免得两处对同一列有两种编码。
func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

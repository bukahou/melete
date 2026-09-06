package localauthx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bukahou/gokit/localauth"
	"github.com/jmoiron/sqlx"

	"github.com/bukahou/melete/backend/internal/userid"
)

// sessionStore 是 localauth.SessionStore 的 MySQL 实现。
type sessionStore struct{ db *sqlx.DB }

var _ localauth.SessionStore = (*sessionStore)(nil)

func NewSessionStore(db *sqlx.DB) localauth.SessionStore { return &sessionStore{db: db} }

// sessionRow 是表结构到模块类型的搬运层。
type sessionRow struct {
	ID           []byte         `db:"id"`
	UserID       []byte         `db:"user_id"`
	CreatedAt    time.Time      `db:"created_at"`
	LastActiveAt time.Time      `db:"last_active_at"`
	ExpiresAt    time.Time      `db:"expires_at"`
	DeviceInfo   sql.NullString `db:"device_info"`
	ClientIP     sql.NullString `db:"client_ip"`
}

func (r sessionRow) toRecord() (localauth.SessionRecord, error) {
	id, err := userid.Decode(r.ID)
	if err != nil {
		return localauth.SessionRecord{}, fmt.Errorf("会话 id: %w", err)
	}
	uid, err := userid.Decode(r.UserID)
	if err != nil {
		return localauth.SessionRecord{}, fmt.Errorf("会话的 user_id: %w", err)
	}
	return localauth.SessionRecord{
		ID: id, UserID: uid,
		CreatedAt: r.CreatedAt, LastActiveAt: r.LastActiveAt, ExpiresAt: r.ExpiresAt,
		DeviceInfo: r.DeviceInfo.String, ClientIP: r.ClientIP.String,
	}, nil
}

const sessionCols = `id, user_id, created_at, last_active_at, expires_at, device_info, client_ip`

func (s *sessionStore) Create(ctx context.Context, rec localauth.SessionRecord, refreshHash []byte) (localauth.SessionRecord, error) {
	uid, err := userid.Encode(rec.UserID)
	if err != nil {
		return localauth.SessionRecord{}, err
	}
	// ⭐ 会话 id 也用 UUIDv7：与账号 id 同一形态，且时间有序 ——
	// 会话表按时间增长，随机 id 会把插入打散到整棵索引树上。
	sid, err := userid.New()
	if err != nil {
		return localauth.SessionRecord{}, fmt.Errorf("生成会话 id: %w", err)
	}
	sidBin, err := userid.Encode(sid)
	if err != nil {
		return localauth.SessionRecord{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_sessions
		  (id, user_id, refresh_hash, device_info, client_ip, created_at, last_active_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sidBin, uid, refreshHash,
		nullStr(rec.DeviceInfo), nullStr(rec.ClientIP),
		rec.CreatedAt, rec.LastActiveAt, rec.ExpiresAt); err != nil {
		return localauth.SessionRecord{}, fmt.Errorf("建立会话: %w", err)
	}
	rec.ID = sid
	return rec, nil
}

// Rotate 轮换 refresh，并区分三种「换不成」的原因。
//
// ⛔⛔ UPDATE 必须按【旧哈希】匹配，⛔ 不能按行 id。
//
// ⚠️ melete 旧代码 internal/token/token.go 正是按 id 匹配的，并且注释断言
// 「并发时另一方拿到 0 行」—— dev 库实测证伪：两个并发 UPDATE 都影响 1 行，
// 于是两个请求各拿到一张新 refresh、库里只留后者，先拿到的那张是死票。
// 按旧哈希匹配之后，第二个 UPDATE 匹配 0 行，竞态消失。
//
// 三种结果的处置【完全不同】，所以判定顺序不能乱：
//
//	Rotated   换成功
//	Revoked   命中当前哈希但会话已死 —— 正常事件，只拒这一次
//	Replayed  命中【上一个】哈希 —— 模块会吊销该用户全部会话（无开关）
//	Unknown   查不出来历 —— 只拒这一次
func (s *sessionStore) Rotate(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (localauth.SessionRecord, localauth.RotateOutcome, error) {
	var (
		rec localauth.SessionRecord
		out localauth.RotateOutcome
	)
	err := runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		type full struct {
			sessionRow
			RevokedAt sql.NullTime `db:"revoked_at"`
		}
		var f full
		err := tx.GetContext(ctx, &f,
			`SELECT `+sessionCols+`, revoked_at FROM user_sessions WHERE refresh_hash = ?`, oldHash)
		switch {
		case err == nil:
			r, cerr := f.sessionRow.toRecord()
			if cerr != nil {
				return cerr
			}
			// ⚠️ 已吊销 / 已过期 → Revoked，⛔ 不是 Replayed。
			// 判成 Replayed 会把该用户全部会话吊掉，而这只是一条已经死了的会话。
			if f.RevokedAt.Valid || !r.ExpiresAt.After(time.Now()) {
				rec, out = r, localauth.RotateRevoked
				return nil
			}
			res, uerr := tx.ExecContext(ctx, `
				UPDATE user_sessions
				   SET prev_refresh_hash = refresh_hash, refresh_hash = ?,
				       expires_at = ?, last_active_at = ?
				 WHERE refresh_hash = ? AND revoked_at IS NULL`,
				newHash, newExpiry, time.Now().UTC(), oldHash)
			if uerr != nil {
				return fmt.Errorf("轮换会话: %w", uerr)
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				// 并发输了：另一方已经把这个哈希换走，此刻它已成为 prev。
				// ⇒ 语义上就是重放（同一个 token 被用了第二次）。
				rec, out = r, localauth.RotateReplayed
				return nil
			}
			r.ExpiresAt = newExpiry
			rec, out = r, localauth.RotateRotated
			return nil

		case errors.Is(err, sql.ErrNoRows):
			// 当前哈希没命中 → 看看是不是【上一个】哈希。
			var p full
			perr := tx.GetContext(ctx, &p,
				`SELECT `+sessionCols+`, revoked_at FROM user_sessions WHERE prev_refresh_hash = ?`, oldHash)
			if errors.Is(perr, sql.ErrNoRows) {
				out = localauth.RotateUnknown
				return nil
			}
			if perr != nil {
				return fmt.Errorf("查询上一个哈希: %w", perr)
			}
			r, cerr := p.sessionRow.toRecord()
			if cerr != nil {
				return cerr
			}
			// ⚠️ Replayed 也必须填上 UserID —— 模块要靠它吊销该用户全部会话。
			rec, out = r, localauth.RotateReplayed
			return nil

		default:
			return fmt.Errorf("查询会话: %w", err)
		}
	})
	return rec, out, err
}

func (s *sessionStore) FindByHash(ctx context.Context, hash []byte) (localauth.SessionRecord, bool, error) {
	var row sessionRow
	err := s.db.GetContext(ctx, &row, `SELECT `+sessionCols+` FROM user_sessions
		WHERE refresh_hash = ? AND revoked_at IS NULL AND expires_at > ?`, hash, time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return localauth.SessionRecord{}, false, nil
	}
	if err != nil {
		return localauth.SessionRecord{}, false, fmt.Errorf("按哈希查会话: %w", err)
	}
	r, cerr := row.toRecord()
	return r, cerr == nil, cerr
}

func (s *sessionStore) RevokeByHash(ctx context.Context, hash []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = ? WHERE refresh_hash = ? AND revoked_at IS NULL`,
		time.Now().UTC(), hash)
	if err != nil {
		return fmt.Errorf("按哈希吊销会话: %w", err)
	}
	return nil
}

// RevokeByID ⛔ 必须同时匹配 userID —— 只按 sessionID 吊销是 IDOR：
// 拿到（或猜到）别人的会话 id 就能把别人登出。
func (s *sessionStore) RevokeByID(ctx context.Context, userID, sessionID string) error {
	uid, err := userid.Encode(userID)
	if err != nil {
		return err
	}
	sid, err := userid.Encode(sessionID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), sid, uid); err != nil {
		return fmt.Errorf("吊销指定会话: %w", err)
	}
	return nil
}

func (s *sessionStore) RevokeAllByUser(ctx context.Context, userID string) (int, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), uid)
	if err != nil {
		return 0, fmt.Errorf("吊销全部会话: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *sessionStore) RevokeOthersByUser(ctx context.Context, userID, keepSessionID string) (int, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return 0, err
	}
	keep, err := userid.Encode(keepSessionID)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = ?
		  WHERE user_id = ? AND id <> ? AND revoked_at IS NULL`,
		time.Now().UTC(), uid, keep)
	if err != nil {
		return 0, fmt.Errorf("登出其它设备: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListByUser 列出该用户当前有效的会话。
// ⛔ 返回值里没有任何 token / 哈希字段 —— 会话列表是给人看的，
// 泄漏哈希等于把撤销凭据摆到界面上。
func (s *sessionStore) ListByUser(ctx context.Context, userID string) ([]localauth.SessionRecord, error) {
	uid, err := userid.Encode(userID)
	if err != nil {
		return nil, err
	}
	var rows []sessionRow
	if err := s.db.SelectContext(ctx, &rows, `SELECT `+sessionCols+` FROM user_sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		ORDER BY last_active_at DESC`, uid, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("列出会话: %w", err)
	}
	out := make([]localauth.SessionRecord, 0, len(rows))
	for _, r := range rows {
		rec, cerr := r.toRecord()
		if cerr != nil {
			return nil, cerr
		}
		out = append(out, rec)
	}
	return out, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

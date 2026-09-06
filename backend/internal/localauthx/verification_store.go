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

// verificationStore 是 localauth.VerificationStore 的 MySQL 实现。
//
// ⛔ 表里只有 verifier 的摘要，没有验证码明文 —— 接口收的就是 []byte 哈希，
// 「存明文」在类型上写不出来。
type verificationStore struct{ db *sqlx.DB }

var _ localauth.VerificationStore = (*verificationStore)(nil)

func NewVerificationStore(db *sqlx.DB) localauth.VerificationStore {
	return &verificationStore{db: db}
}

// Issue 写入新记录，并先作废同 (subject, purpose) 的未消费行。
//
// ⭐ 一人一码，重发即覆盖。⚠️ 不作废旧行的后果不是"多几行垃圾"：
// 用户手里同时有效的码会越积越多，每一个都能通过校验 ——
// 而"重发一次"是攻击者也能触发的操作。
//
// ⚠️ 作废与写入必须同一个事务：分开做的话，两者之间存在一个
// 【一个码都没有】的窗口，并发重发会让先到的那个码被后到的作废掉，
// 而后到的那封信可能永远送不到用户手里。
func (s *verificationStore) Issue(ctx context.Context, rec localauth.VerificationRecord, verifierHash []byte) (string, error) {
	id, err := userid.New()
	if err != nil {
		return "", fmt.Errorf("生成验证记录 id: %w", err)
	}
	idBin, err := userid.Encode(id)
	if err != nil {
		return "", err
	}
	err = runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE verification_tokens SET consumed_at = ?
			 WHERE purpose = ? AND subject = ? AND consumed_at IS NULL`,
			time.Now().UTC(), string(rec.Purpose), rec.Subject); err != nil {
			return fmt.Errorf("作废旧验证码: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO verification_tokens
			  (id, purpose, subject, payload, verifier_hash, attempts, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			idBin, string(rec.Purpose), rec.Subject, nullStr(rec.Payload),
			verifierHash, rec.Attempts, time.Now().UTC(), rec.ExpiresAt); err != nil {
			return fmt.Errorf("写入验证记录: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

type verificationRow struct {
	ID           []byte         `db:"id"`
	Purpose      string         `db:"purpose"`
	Subject      string         `db:"subject"`
	Payload      sql.NullString `db:"payload"`
	VerifierHash []byte         `db:"verifier_hash"`
	Attempts     int            `db:"attempts"`
	ExpiresAt    time.Time      `db:"expires_at"`
}

func (r verificationRow) toRecord() (localauth.VerificationRecord, error) {
	id, err := userid.Decode(r.ID)
	if err != nil {
		return localauth.VerificationRecord{}, fmt.Errorf("验证记录 id: %w", err)
	}
	return localauth.VerificationRecord{
		ID: id, Purpose: localauth.TokenPurpose(r.Purpose), Subject: r.Subject,
		Payload: r.Payload.String, Attempts: r.Attempts, ExpiresAt: r.ExpiresAt,
	}, nil
}

// FindPending 取未消费、未过期的那一行。
//
// ⚠️ 这里【也】过滤过期行。守卫自己会查 ExpiresAt，所以不过滤同样合法，
// 但过滤掉能少走一段路，且让"过期"与"不存在"在存储层就归一 ——
// ⛔ 两者对调用方本来就该是同一件事。
func (s *verificationStore) FindPending(ctx context.Context, purpose localauth.TokenPurpose, subject string) (localauth.VerificationRecord, []byte, bool, error) {
	var row verificationRow
	err := s.db.GetContext(ctx, &row, `
		SELECT id, purpose, subject, payload, verifier_hash, attempts, expires_at
		  FROM verification_tokens
		 WHERE purpose = ? AND subject = ? AND consumed_at IS NULL AND expires_at > ?
		 ORDER BY created_at DESC LIMIT 1`,
		string(purpose), subject, time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return localauth.VerificationRecord{}, nil, false, nil
	}
	if err != nil {
		return localauth.VerificationRecord{}, nil, false, fmt.Errorf("查询待验证记录: %w", err)
	}
	rec, cerr := row.toRecord()
	if cerr != nil {
		return localauth.VerificationRecord{}, nil, false, cerr
	}
	return rec, row.VerifierHash, true, nil
}

// BumpAttempts 原子 +1，返回自增之后的值。
//
// ⚠️ 与 FailureStore.Bump 同一条理由：自增与回读必须同属一个原子区间，
// 否则并发时返回的不是"我这一次"的值，而尝试次数上限正是靠它判定的。
func (s *verificationStore) BumpAttempts(ctx context.Context, id string) (int, error) {
	idBin, err := userid.Encode(id)
	if err != nil {
		return 0, err
	}
	var n int
	err = runInTx(ctx, s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE verification_tokens SET attempts = attempts + 1 WHERE id = ?`, idBin); err != nil {
			return fmt.Errorf("自增尝试次数: %w", err)
		}
		if err := tx.GetContext(ctx, &n,
			`SELECT attempts FROM verification_tokens WHERE id = ?`, idBin); err != nil {
			return fmt.Errorf("回读尝试次数: %w", err)
		}
		return nil
	})
	return n, err
}

// Consume 一次性消费。
//
// ⭐ 单条原子语句，以受影响行数判定 —— `consumed_at IS NULL` 在 WHERE 里，
// 所以两个并发请求拿同一个码时只有一个能改到这一行。
//
// ⚠️ 这与 SessionStore.Rotate 是同一个形状：条件写进 WHERE，用 RowsAffected
// 判胜负。⛔ 反面教材就在本仓 internal/token/token.go —— 它把条件放在
// 先前的 SELECT 里、UPDATE 只按 id 匹配，于是两个并发请求都"成功"了。
func (s *verificationStore) Consume(ctx context.Context, id string, at time.Time) (bool, error) {
	idBin, err := userid.Encode(id)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE verification_tokens SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL`,
		at, idBin)
	if err != nil {
		return false, fmt.Errorf("消费验证码: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

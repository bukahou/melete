package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("account not found")

// Repository 是账号数据的读写契约。
type Repository interface {
	FindByUsername(ctx context.Context, username string) (*Account, error)
	FindByID(ctx context.Context, id int64) (*Account, error)
	// UpsertByAkashaSub 幂等确立联邦账号：不存在则建，存在则刷新 display。
	UpsertByAkashaSub(ctx context.Context, sub, display string) (*Account, error)
}

type mysqlRepository struct{ db *sqlx.DB }

func NewMySQLRepository(db *sqlx.DB) Repository { return &mysqlRepository{db: db} }

const cols = `id, akasha_sub, username, password_hash, display`

func (r *mysqlRepository) FindByUsername(ctx context.Context, username string) (*Account, error) {
	var a Account
	err := r.db.GetContext(ctx, &a, `SELECT `+cols+` FROM account WHERE username = ?`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询账号 %q: %w", username, err)
	}
	return &a, nil
}

func (r *mysqlRepository) FindByID(ctx context.Context, id int64) (*Account, error) {
	var a Account
	err := r.db.GetContext(ctx, &a, `SELECT `+cols+` FROM account WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询账号 %d: %w", id, err)
	}
	return &a, nil
}

func (r *mysqlRepository) UpsertByAkashaSub(ctx context.Context, sub, display string) (*Account, error) {
	// 不假设 id 连续、不依赖 LAST_INSERT_ID 的跨行为差异（TiDB 纪律），
	// upsert 后回查一次。
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO account (akasha_sub, display) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE display = VALUES(display)`, sub, display)
	if err != nil {
		return nil, fmt.Errorf("upsert 联邦账号: %w", err)
	}
	var a Account
	if err := r.db.GetContext(ctx, &a, `SELECT `+cols+` FROM account WHERE akasha_sub = ?`, sub); err != nil {
		return nil, fmt.Errorf("回查联邦账号: %w", err)
	}
	return &a, nil
}

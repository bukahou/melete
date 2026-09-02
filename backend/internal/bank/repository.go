package bank

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// ErrNotFound 由 Repository 返回，让上层无需感知 sql.ErrNoRows。
var ErrNotFound = errors.New("bank not found")

// Repository 是题库数据的读取契约。
// 定义在使用方这一侧，实现可替换（测试用内存实现即可）。
type Repository interface {
	ListBanks(ctx context.Context) ([]Bank, error)
	FindBankBySlug(ctx context.Context, slug string) (*Bank, error)
	LoadStats(ctx context.Context, bankID int64) (*Stats, error)
	ListTags(ctx context.Context, bankID int64, tagType string) ([]Tag, error)
}

type mysqlRepository struct{ db *sqlx.DB }

func NewMySQLRepository(db *sqlx.DB) Repository { return &mysqlRepository{db: db} }

const bankColumns = `id, slug, name, description, locale, kind`

func (r *mysqlRepository) ListBanks(ctx context.Context) ([]Bank, error) {
	banks := []Bank{}
	err := r.db.SelectContext(ctx, &banks, `SELECT `+bankColumns+` FROM bank ORDER BY id`)
	return banks, err
}

func (r *mysqlRepository) FindBankBySlug(ctx context.Context, slug string) (*Bank, error) {
	var b Bank
	err := r.db.GetContext(ctx, &b, `SELECT `+bankColumns+` FROM bank WHERE slug = ?`, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询题库 %q: %w", slug, err)
	}
	return &b, nil
}

// LoadStats 统计题目总数、已富化数、以及**存在答案分歧的题数**。
//
// 最后一个是本项目的核心指标：题库标注与社区投票不一致的比例接近四成，
// 照搬标注答案会背错三分之一 —— 这个数字要直接呈现给学习者。
func (r *mysqlRepository) LoadStats(ctx context.Context, bankID int64) (*Stats, error) {
	var s Stats
	err := r.db.GetContext(ctx, &s, `
		SELECT
		  COUNT(*) AS question_count,
		  COALESCE(SUM(EXISTS(
		    SELECT 1 FROM explanation e WHERE e.question_id = q.id
		  )), 0) AS enriched_count,
		  COALESCE(SUM(EXISTS(
		    SELECT 1 FROM answer_claim bl
		    JOIN answer_claim cv
		      ON cv.question_id = bl.question_id AND cv.source = 'community_vote'
		    WHERE bl.question_id = q.id AND bl.source = 'bank_label'
		      AND bl.answer <> cv.answer
		  )), 0) AS contested_count
		FROM question q WHERE q.bank_id = ?`, bankID)
	if err != nil {
		return nil, fmt.Errorf("统计题库 %d: %w", bankID, err)
	}
	return &s, nil
}

// ListTags 返回该题库可见的标签：题库私有的 + 全局共享的（bank_id = 0）。
func (r *mysqlRepository) ListTags(ctx context.Context, bankID int64, tagType string) ([]Tag, error) {
	query := `
		SELECT t.id, t.bank_id, t.type, t.value, t.i18n, COUNT(qt.question_id) AS question_count
		FROM tag t
		JOIN question_tag qt ON qt.tag_id = t.id
		JOIN question q      ON q.id = qt.question_id AND q.bank_id = ?
		WHERE t.bank_id IN (0, ?)`
	args := []any{bankID, bankID}
	if tagType != "" {
		query += ` AND t.type = ?`
		args = append(args, tagType)
	}
	query += ` GROUP BY t.id ORDER BY t.type, question_count DESC, t.value`

	tags := []Tag{}
	if err := r.db.SelectContext(ctx, &tags, query, args...); err != nil {
		return nil, fmt.Errorf("查询题库 %d 的标签: %w", bankID, err)
	}
	return tags, nil
}

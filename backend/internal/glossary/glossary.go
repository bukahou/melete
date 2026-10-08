// Package glossary 是用语集（P9 第 6 步，裁决 #5）。
//
// 术语由生成管道从题目里抽出（2026-10-08 用户裁定「从题目里抽」），一个题库一套；
// term_question 记录「这个词在哪些题考过」（it-pass 的出題歴）。
// 本包只读 —— 内容由 pipeline 的 load 写入，应用侧不改术语。
package glossary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// ErrNotFound 表示术语不存在。
var ErrNotFound = errors.New("term not found")

// Summary 是目录里的一条：不带释义 —— 目录要一次给全（前端本地即时检索），体积要小。
type Summary struct {
	ID            int64             `db:"id"`
	Slug          string            `db:"slug"`
	Names         map[string]string `db:"-"`
	NamesRaw      []byte            `db:"names"`
	Reading       *string           `db:"reading"`
	Category      string            `db:"category"`
	Lead          bool              `db:"is_lead"` // 分类的主条目（EC2 分类里的「Amazon EC2」）
	QuestionCount int               `db:"question_count"`
}

// QuestionRef 是「在哪些题出过」的一条。
type QuestionRef struct {
	ID         int64  `db:"id"`
	ExternalNo int    `db:"external_no"`
	Session    string `db:"session"`
	Stem       string `db:"stem"`
}

// Detail 是单个术语：释义 + 出题历史。
type Detail struct {
	Summary
	BankSlug   string            `db:"bank_slug"`
	Definition map[string]string `db:"-"`
	DefRaw     []byte            `db:"definition"`
	Questions  []QuestionRef     `db:"-"`
}

// Service 是用语集的门面。
type Service interface {
	ListTerms(ctx context.Context, bankID int64) ([]Summary, error)
	// GetTerm 的 locale 用来挑题干译文（出题历史里显示的题干）；空串 = 源语言。
	GetTerm(ctx context.Context, id int64, locale string) (*Detail, error)
}

type service struct{ db *sqlx.DB }

func NewService(db *sqlx.DB) Service { return &service{db: db} }

func (s *service) ListTerms(ctx context.Context, bankID int64) ([]Summary, error) {
	out := []Summary{}
	err := s.db.SelectContext(ctx, &out, `
		SELECT t.id, t.slug, t.names, t.reading, t.category, t.is_lead,
		       (SELECT COUNT(*) FROM term_question tq WHERE tq.term_id = t.id) AS question_count
		FROM term t WHERE t.bank_id = ?
		ORDER BY t.category, t.slug`, bankID)
	if err != nil {
		return nil, fmt.Errorf("查询术语目录: %w", err)
	}
	for i := range out {
		out[i].Names = decode(out[i].NamesRaw)
	}
	return out, nil
}

func (s *service) GetTerm(ctx context.Context, id int64, locale string) (*Detail, error) {
	var d Detail
	err := s.db.GetContext(ctx, &d, `
		SELECT t.id, t.slug, t.names, t.reading, t.category, t.is_lead, t.definition, b.slug AS bank_slug,
		       (SELECT COUNT(*) FROM term_question tq WHERE tq.term_id = t.id) AS question_count
		FROM term t JOIN bank b ON b.id = t.bank_id WHERE t.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询术语 %d: %w", id, err)
	}
	d.Names, d.Definition = decode(d.NamesRaw), decode(d.DefRaw)
	// 出题历史的题干：有该语言的译文就用译文（与刷题页同一个回退规则）
	d.Questions = []QuestionRef{}
	err = s.db.SelectContext(ctx, &d.Questions, `
		SELECT q.id, q.external_no, q.session, COALESCE(qi.stem, q.stem) AS stem
		FROM term_question tq
		JOIN question q ON q.id = tq.question_id
		LEFT JOIN question_i18n qi ON qi.question_id = q.id AND qi.locale = ?
		WHERE tq.term_id = ?
		ORDER BY q.session, q.external_no`, locale, id)
	if err != nil {
		return nil, fmt.Errorf("查询术语 %d 的出题历史: %w", id, err)
	}
	return &d, nil
}

// decode 把 JSON 列解成 map；坏数据给空 map —— 一个术语的名字缺失不该让整页报错。
func decode(raw []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(raw, &m)
	return m
}

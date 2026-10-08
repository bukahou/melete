package httpapi

import (
	"context"
	"encoding/json"

	"github.com/bukahou/melete/backend/internal/api"
	"github.com/bukahou/melete/backend/internal/httpauth"
	"github.com/bukahou/melete/backend/internal/question"
	"github.com/bukahou/melete/backend/internal/userid"
)

// drillQuery 是「一个题目集合」在适配层的统一形状（P9 第 4 步）。
//
// ⭐ 三个来源都先转成它，再转成 question.ListFilter —— 只有一处翻译：
//   - GET /banks/{slug}/questions 的查询参数（刷题页取题）
//   - GET /banks/{slug}/questions/summary 的查询参数（本轮小结）
//   - attempt.context（首页「继续」从最近一次作答重建入口）
//
// ⛔ 三处各写一份的后果：「继续」算出的位置与刷题页实际出的题对不上，而且不报错。
type drillQuery struct {
	Tag        []int64
	AnyTag     []int64
	Contested  bool
	Bookmarked bool
	Mode       string
	Session    *string
	NoFrom     int
	NoTo       int
	Seed       *int64
	Take       int
}

func (d drillQuery) filter(account userid.UserID) question.ListFilter {
	return question.ListFilter{
		TagIDs: d.Tag, AnyTagIDs: d.AnyTag,
		OnlyContested: d.Contested, OnlyBookmarked: d.Bookmarked,
		Mode: d.Mode, AccountID: account,
		Session: d.Session, NoFrom: d.NoFrom, NoTo: d.NoTo,
		Seed: d.Seed, Take: d.Take,
	}
}

func deref[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return v
}

// drillQueryFromContext 把作答出处翻回它的题目集合。
// ok=false 表示这条出处重建不出集合（坏数据 / 缺必填字段）—— 调用方当作没有游标。
func drillQueryFromContext(c api.DrillContext) (drillQuery, bool) {
	d := drillQuery{Session: c.Session, NoFrom: deref(c.NoFrom), NoTo: deref(c.NoTo)}
	switch c.Mode {
	// ---- P9 的四个入口 ----
	case api.DrillContextModeYear:
		// 一套卷子或一段题号；两样都没有就是整个题库 —— 那不是 4.1 会产生的出处
		if c.Session == nil && c.NoFrom == nil && c.NoTo == nil {
			return d, false
		}
	case api.DrillContextModeDomain:
		if c.TagIds == nil || len(*c.TagIds) == 0 {
			return d, false
		}
		d.AnyTag = *c.TagIds
	case api.DrillContextModePick:
		d.AnyTag = deref(c.TagIds)
		d.Seed = c.Seed // 4.3 列表的「打乱顺序」：同一筛选结果按种子固定打乱
		switch deref(c.Status) {
		case api.DrillContextStatusWrong:
			d.Mode = "wrong"
		case api.DrillContextStatusUnseen:
			d.Mode = "unseen"
		case api.DrillContextStatusBookmarked:
			d.Bookmarked = true
		case api.DrillContextStatusContested:
			d.Contested = true
		}
	case api.DrillContextModeRandom:
		if c.Seed == nil || c.Count == nil || *c.Count <= 0 {
			return d, false
		}
		d.Seed, d.Take = c.Seed, *c.Count
	// ---- P9 之前的入口：旧记录照常重建 ----
	case api.DrillContextModeTag:
		if c.TagId == nil {
			return d, false
		}
		d.Tag = []int64{*c.TagId}
	case api.DrillContextModeContested:
		d.Contested = true
	case api.DrillContextModeWrong, api.DrillContextModeUnsure, api.DrillContextModeUnseen, api.DrillContextModeDue:
		d.Mode = string(c.Mode)
	case api.DrillContextModeAll:
	default:
		return d, false
	}
	return d, true
}

// loadCursor 算出单一继续槽位（P9 #13）：最近一次作答的入口 + 该从第几题接着做。
// 从没作答过、或最近那条出处重建不出集合（坏数据 / 旧客户端没带 context）⇒ nil。
func (s *Server) loadCursor(ctx context.Context, accountID userid.UserID, slug string) (*api.DrillCursor, error) {
	last, err := s.studies.LoadLastAttempt(ctx, accountID, slug)
	if err != nil || last == nil || last.Context == nil {
		return nil, err
	}
	var c api.DrillContext
	if json.Unmarshal([]byte(*last.Context), &c) != nil {
		return nil, nil // 一行脏 JSON 不该让首页报错
	}
	d, ok := drillQueryFromContext(c)
	if !ok {
		return nil, nil
	}
	b, err := s.banks.FindBank(ctx, slug)
	if err != nil {
		return nil, err
	}
	offset, total, err := s.questions.LocateAfter(ctx, b.ID, d.filter(accountID), last.QuestionID)
	if err != nil {
		return nil, err
	}
	cur := &api.DrillCursor{
		Context: c, LastAt: last.At, Offset: offset, Total: total,
		// 按顺序的集合做到最后一题 ⇒「继续」给本轮小结；会缩短的集合 offset 恒为 0，空了也是 finished
		Finished: offset >= total,
	}
	return cur, nil
}

// accountOf 取已认证账号；未认证返回空串（个人化条件会在 question 服务里被拒）。
func accountOf(ctx context.Context) userid.UserID {
	if id, ok := httpauth.AccountID(ctx); ok {
		return userid.UserID(id)
	}
	return ""
}

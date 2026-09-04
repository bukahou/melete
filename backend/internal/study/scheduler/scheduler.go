package scheduler

import (
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs/v3"
)

// State 是卡片的调度状态，取值与 card.state 列一一对应。
type State int8

const (
	StateNew        State = 0 // 从没做过
	StateLearning   State = 1 // 初学，当天内多次
	StateReview     State = 2 // 已进入长间隔复习
	StateRelearning State = 3 // 复习时失败，重新拉回短间隔
)

func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateLearning:
		return "learning"
	case StateReview:
		return "review"
	case StateRelearning:
		return "relearning"
	}
	return "unknown"
}

// Card 是一张卡片的记忆状态，字段与 card 表逐列对应。
//
// ⚠️ 刻意【不】保存 fsrs.Card 的 ElapsedDays / ScheduledDays 两个字段：
// 读 go-fsrs v3.3.1 的 scheduler.go:76-80 可见，ElapsedDays 每次调度时都由
// `now - LastReview` 重新算出，持久化的值会被直接覆盖；ScheduledDays 则只被
// 写入、从不作为输入参与运算。存它们等于存两份同一件事，而副本会漂移。
//
//	if s.current.State != New && !s.current.LastReview.IsZero() {
//	    interval = math.Floor(s.now.Sub(s.current.LastReview).Hours() / 24)
//	}
//	s.current.ElapsedDays = uint64(interval)
//
// 所以 LastReview 才是真正必须持久化的那一个。
type Card struct {
	State      State
	Due        time.Time
	Stability  float64
	Difficulty float64
	Reps       int
	Lapses     int
	// LastReview 为零值表示从未复习过（新卡）。
	LastReview time.Time
}

// NewCard 返回一张没做过的新卡。
func NewCard() Card {
	return fromFSRS(fsrs.NewCard())
}

// Scheduler 是记忆调度的门面 —— 调用方只见 Card 与四档评分，
// 见不到 fsrs 的任何类型。
//
// 这样做的理由不是洁癖：将来若要换调度算法、或用用户自己的历史重新拟合参数，
// 换的只是这个文件；study 域与 API 契约一个字不用动。
type Scheduler struct {
	f *fsrs.FSRS
}

// New 用 FSRS 官方默认参数构造调度器。
//
// ⚠️ 默认参数是在大规模公开复习数据上拟合出来的，目标留存率 0.90。
// ⛔ 不要手改那 19 个权重 —— 它们是一起拟合的，单独动一个没有意义。
// 将来要个性化，正确做法是用本人的复习历史整体重新拟合（FSRS optimizer 的活）。
func New() *Scheduler {
	return &Scheduler{f: fsrs.NewFSRS(fsrs.DefaultParam())}
}

// Next 算出这次评分之后卡片的新状态。
//
// rating 取 1-4，应当是 EffectiveRating 纠正之后的值 ——
// ⚠️ 不要把用户按的原始键直接送进来，那正是「嘴硬」能污染调度的入口。
func (s *Scheduler) Next(c Card, rating int, now time.Time) Card {
	return fromFSRS(s.f.Next(toFSRS(c), now, fsrs.Rating(rating)).Card)
}

// Retrievability 是此刻还记得的概率（0-1）。它不持久化，永远现算。
func (s *Scheduler) Retrievability(c Card, now time.Time) float64 {
	return s.f.GetRetrievability(toFSRS(c), now)
}

func toFSRS(c Card) fsrs.Card {
	return fsrs.Card{
		Due:        c.Due,
		Stability:  c.Stability,
		Difficulty: c.Difficulty,
		Reps:       uint64(c.Reps),
		Lapses:     uint64(c.Lapses),
		State:      fsrs.State(c.State),
		LastReview: c.LastReview,
		// ElapsedDays / ScheduledDays 交给库自己算，见 Card 的注释
	}
}

func fromFSRS(c fsrs.Card) Card {
	return Card{
		State:      State(c.State),
		Due:        c.Due,
		Stability:  c.Stability,
		Difficulty: c.Difficulty,
		Reps:       int(c.Reps),
		Lapses:     int(c.Lapses),
		LastReview: c.LastReview,
	}
}

package bank

// Bank 是一个题库。SAA-C03 是第一个，不是唯一一个 ——
// 所以这里不出现任何 AWS / 认证考试特有的字段。
type Bank struct {
	ID          int64   `db:"id"`
	Slug        string  `db:"slug"`
	Name        string  `db:"name"`
	Description *string `db:"description"`
	Locale      string  `db:"locale"`
	Kind        string  `db:"kind"`
	// Meta 是题库自描述的展示元数据（标签轴名 / 考纲权重 / 及格线），原样透传给 API。
	// 领域层不解析它：这些词只有前端要用，后端不该认识「考纲域」。
	Meta *string `db:"meta"`
}

// Stats 是题库的内容侧统计，与具体用户无关。
type Stats struct {
	QuestionCount  int `db:"question_count"`
	EnrichedCount  int `db:"enriched_count"`
	ContestedCount int `db:"contested_count"`
}

// Tag 采用通用 (type, value) 结构。
// BankID 为 0 表示全局标签（concept 类，跨题库共享）。
type Tag struct {
	ID            int64   `db:"id"`
	BankID        int64   `db:"bank_id"`
	Type          string  `db:"type"`
	Value         string  `db:"value"`
	I18n          *string `db:"i18n"`
	QuestionCount int     `db:"question_count"`
}

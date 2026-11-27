package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bukahou/melete/backend/internal/study"
)

// 作答推移的 JSON 形状（客户端按它解码）：日期是 YYYY-MM-DD，没作答的桶不带 rate，没有桶时是 [] 而不是 null。
func TestActivityJSONShape(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	a := &study.Activity{
		Range: study.ActivityRangeAll, Unit: study.BucketWeek, TimeZone: "Asia/Tokyo",
		Total: study.ActivityStats{AttemptCount: 3, CorrectCount: 2},
		Buckets: []study.ActivityBucket{
			{Start: d("2026-11-16"), End: d("2026-11-22")},
			{Start: d("2026-11-23"), End: d("2026-11-29"), ActivityStats: study.ActivityStats{AttemptCount: 3, CorrectCount: 2}},
		},
	}
	got, err := json.Marshal(toAPIActivity(a))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"bucket":"week","buckets":[` +
		`{"attemptCount":0,"correctCount":0,"date":"2026-11-16","endDate":"2026-11-22","wrongCount":0},` +
		`{"attemptCount":3,"correctCount":2,"date":"2026-11-23","endDate":"2026-11-29","rate":67,"wrongCount":1}],` +
		`"range":"all","timezone":"Asia/Tokyo","total":{"attemptCount":3,"correctCount":2,"rate":67,"wrongCount":1}}`
	if string(got) != want {
		t.Fatalf("JSON 得到\n%s\n期望\n%s", got, want)
	}

	empty, _ := json.Marshal(toAPIActivity(&study.Activity{Range: study.ActivityRangeAll, Unit: study.BucketDay, TimeZone: "Asia/Tokyo", Buckets: []study.ActivityBucket{}}))
	var m map[string]json.RawMessage
	if err := json.Unmarshal(empty, &m); err != nil || string(m["buckets"]) != "[]" {
		t.Fatalf("没有桶时 buckets 应为 []，得到 %s", m["buckets"])
	}
}

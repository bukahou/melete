package study

import (
	"fmt"
	"time"
	_ "time/tzdata" // 时区库嵌进二进制：运行镜像里不一定有 /usr/share/zoneinfo
)

// studyTZ 是切「自然日」用的时区：连续天数、作答推移都按它。
// 全部按日计的统计只从这里取时区（Go 侧直接用，SQL 侧经 sqlOffset）—— 改时区只改这一处。
var studyTZ = mustLoadLocation("Asia/Tokyo")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("加载时区 %s: %v", name, err))
	}
	return loc
}

// sqlOffset 是 studyTZ 在 t 时刻的 UTC 偏移，写成 CONVERT_TZ 认的「+09:00」。
//
// 用数字偏移而不是时区名：数据库不必装时区表。代价是一条查询里只有一个偏移 ——
// 有夏令时的时区，跨过切换日的那段统计会差一小时；换成那种时区时要改成逐行换算。
func sqlOffset(t time.Time) string {
	return t.In(studyTZ).Format("-07:00")
}

// localDate 是 t 在 studyTZ 里的日历日，表示成那一天 00:00 的 UTC 时间。
// 日期之间加减天数用 UTC 算：UTC 没有夏令时，一天永远是 24 小时。
func localDate(t time.Time) time.Time {
	y, m, d := t.In(studyTZ).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// dayStartUTC 是日历日 day（localDate 的表示法）在 studyTZ 里 00:00 那一刻，换成 UTC。
func dayStartUTC(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, studyTZ).UTC()
}

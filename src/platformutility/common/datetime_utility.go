/*******************************************************
* @Summary:
*	日期工具类，以及函数定义
*
*	这里实现了singleton模式,但是 并不能阻止使用者 new DatetimeUtility()
*	方式创建一个实例。
* @Author:
*	Frank Ng
* @Date:
* 	Jun 19, 2026
*******************************************************/

package common

import (
	"time"
)

func TimestampText(timestamp int64) string {
	if timestamp <= 0 {
		return GetDatetimeUtilityInstance().getCurrentDatetime().DatetimeStr
	}

	return time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
}

// MilliDiff 获取与当前时间的毫秒数
func MilliDiff(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

// weekdayCN 中文星期名(下标与 time.Weekday 对齐,0=周日)
var weekdayCN = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// FormatCurrentTime 渲染"当前时间"注入文本(注入在 system prompt 末尾)。
//
// 为什么必须注入:模型自身不知道"今天"是几号,而历史回放里的工具结果/消息不带时间语义时
// 它无法分辨新旧,会把旧结果当当下复述。文本给出完整日期+星期+时区,
// 并明确要求"相对日期以当前时间为准",供模型做"今天/明天/后天"的换算与新旧核对。
func FormatCurrentTime() string {
	now := time.Now().In(time.Local)
	return now.Format("2006-01-02 15:04:05 -0700") +
		"(" + weekdayCN[now.Weekday()] + ", " + now.Format("MST") + ")" +
		"。用户说的\"今天/明天/后天\"一律以此为准;历史消息与工具结果带的是各自的记录时间," +
		"可能已经过期 —— 引用任何历史数据前,先与当前时间核对。"
}

type DatetimeUtility struct {
	timezone *time.Location
}

var datetimeUtilityInstance = GetSingleton(func() *DatetimeUtility {
	return &DatetimeUtility{timezone: time.Local}
}).Instance()

func GetDatetimeUtilityInstance() *DatetimeUtility {
	return datetimeUtilityInstance
}

func (u *DatetimeUtility) GetCurrentDatetime() *DatetimeItem {
	datetimeItem := u.getCurrentDatetime()
	return datetimeItem
}

func (u *DatetimeUtility) Until(s, e int64) time.Duration {
	diff := e - s
	return time.Duration(diff) * time.Second
}

// GetCurrentDatetime 获取当前系统时间
func (u *DatetimeUtility) getCurrentDatetime() *DatetimeItem {
	now := time.Now().In(u.timezone)

	return &DatetimeItem{
		DatetimeStr: now.Format("2006-01-02 15:04:05"),
		DateStr:     now.Format("2006-01-02"),
		TimeStr:     now.Format("15:04:05"),
		Year:        now.Year(),
		Month:       int(now.Month()),
		Day:         now.Day(),
		Hour:        now.Hour(),
		Minute:      now.Minute(),
		Second:      now.Second(),
		Weekday:     int(now.Weekday()),
		YearDay:     now.YearDay(),

		Timestamp:      now.Unix(),
		MilliTimestamp: now.UnixMilli(),
		MicroTimestamp: now.UnixMicro(),
		NanoTimestamp:  now.UnixNano(),
	}
}

type DatetimeItem struct {
	DatetimeStr string
	DateStr     string
	TimeStr     string

	Year   int
	Month  int
	Day    int
	Hour   int
	Minute int
	Second int

	Timestamp      int64
	MilliTimestamp int64
	MicroTimestamp int64
	NanoTimestamp  int64

	Weekday int
	YearDay int
}

func (d *DatetimeItem) Equals(right *DatetimeItem) bool {
	return d.Timestamp == right.Timestamp
}

// Distance /*
// > 0 表示未来时间比当前时间晚
// < 0 未来时间比当前时间早*/
func (d *DatetimeItem) Distance(other *DatetimeItem, distance int64) int64 {
	return d.MilliTimestamp - other.MilliTimestamp - distance
}

// Until 表示到目标时间t的秒数
func (d *DatetimeItem) Until(t int64) time.Duration {
	diff := t - d.Timestamp
	return time.Duration(diff) * time.Second
}

func (d *DatetimeItem) Add(seconds int64) int64 {
	return time.Now().Add(time.Duration(seconds) * time.Second).Unix()
}

func (d *DatetimeItem) IsBefore(other *DatetimeItem) bool {
	return d.MilliTimestamp < other.MilliTimestamp
}

func (d *DatetimeItem) IsAfter(other *DatetimeItem) bool {
	return d.MilliTimestamp > other.MilliTimestamp
}

func (d *DatetimeItem) BeforeNow(distance int64) bool {
	currentNow := datetimeUtilityInstance.getCurrentDatetime()
	return currentNow.Distance(d, distance) > 0
}

func (d *DatetimeItem) AfterNow(distance int64) bool {
	currentNow := datetimeUtilityInstance.getCurrentDatetime()
	return currentNow.Distance(d, distance) < 0
}

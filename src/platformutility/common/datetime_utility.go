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
type DatetimeUtility struct {
	timezone *time.Location
}

var datetimeUtilityInstance = GetSingleton(func() *DatetimeUtility {
	return &DatetimeUtility{timezone: time.Local}
}).Instance()

func GetDatetimeUtilityInstance() *DatetimeUtility {
	return datetimeUtilityInstance
}

func (d *DatetimeUtility) GetCurrentDatetime() *DatetimeItem {
	datetimeItem := d.getCurrentDatetime()
	return datetimeItem
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

// ============================= private functions & methods ==============================

// GetCurrentDatetime 获取当前系统时间
func (d *DatetimeUtility) getCurrentDatetime() *DatetimeItem {
	now := time.Now().In(d.timezone)

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

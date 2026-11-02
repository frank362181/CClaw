---
name: weather
description: 查询实时天气与未来三天预报，返回 JSON。回答天气问题必须实时调用 get_weather 取数（历史结果可能已过期，不得复用）。
# 触发词(意图门):命中才强制调用 get_weather 并注入本技能正文。
# 刻意只收“真在问天气”的词,不收「晴/阴/冷/热」这种单字(「冷启动」「阴影」会误命中);
# 收起会重新变成“agent 级强制”——写报告时被逼着调天气工具。
trigger-keywords:
  - 天气
  - 天气预报
  - 气温
  - 温度
  - 多少度
  - 几度
  - 体感
  - 下雨
  - 降雨
  - 雨
  - 下雪
  - 风力
  - 冷不冷
  - 热不热
user-invocable: true
disable-mode-invocation: false
disallowed-tools: ["edit","bash"]
emoji: 🌤️
home-page:
os:
  - darwin
  - linux
  - windows
requires:
  bins:
    - python3
tools:
  - name: get_weather
    description: >-
      查询指定城市的实时天气与未来三天预报，返回 JSON。城市名支持中文/英文，可带"市/县/区/州"等行政后缀（如"杭州市"）。
      【必须实时调用】回答任何天气相关问题前都必须调用本工具取数：上下文/历史里的工具结果可能已过期，
      严禁复用旧结果、严禁凭记忆或日期推算作答。不传日期参数时默认返回"今天"起三天；
      返回体顶层 today 为**地点本地日期**，用它校对"今天/明天/后天"，不要用历史消息里的日期。
    script: scripts/weather.py
    force-call: true
    parameters:
      type: object
      properties:
        city:
          type: string
          description: 城市名称，如：杭州、温州市、北京、Guangzhou。单名城市不带后缀也能查到（脚本会自动补"市"）。
        day_offset:
          type: integer
          description: 相对天数偏移（可选）：0=今天、1=明天、2=后天。用户说"今天/明天/后天"时传它；与 date 二选一。
        date:
          type: string
          description: 目标日期 YYYY-MM-DD（可选）。用户明确给出日期时传它；必须落在返回窗口内，否则报错并给出可用区间。
      required:
        - city
---

# 天气预报技能

调用 `scripts/weather.py` 查询实时天气与未来三天预报，结果以 JSON 输出。

## 执行 SOP （standard operation procedure）

1. 从用户输入的请求中提取城市，和日期："今天/明天/后天" → `day_offset` 0/1/2；明确日期 → `date`（YYYY-MM-DD；不确定就留空 = 今天）
2. 调用 `get_weather` —— **必须实时调用**：历史工具结果可能已过期，不得复用、不得凭记忆作答
3. 用返回的 `today` 校对日期，再按 `forecast[].date` 组织回答；做旅行计划时从明天（`day_offset=1`）开始

## 调用方式

- 执行器把参数 JSON 写入脚本 stdin：`{"city": "杭州市"}`、`{"city": "温州市", "day_offset": 1}`、`{"city": "杭州", "date": "2026-09-19"}`
- 脚本把结果 JSON 写到 **stdout**，退出码 0 表示成功
- 失败时退出码非 0，stderr 为错误信息（stdout 为 `{"error": "..."}`）；日期越界等参数错误会给出可用区间，改正后重试即可

## 城市名兼容

- 支持带行政后缀的城市名：`杭州市`、`北京市`、`那曲市`、`凉山自治州`、`舟山群岛`等
- 原始名匹配不到时自动剥离后缀（市/县/区/州/群岛…）再试；剥离后过短（≤1 字）则保留原样
- 单名城市会自动补"市"再试（`温州` → `温州市`，地理库里只有后者）
- 命中多个同名地点时优先取中国境内结果
- 日期格式为：YYYY-MM-DD：`2026-09-04`

## 输出 JSON 结构

```json
{
  "city": "杭州市",
  "today": "2026-09-17",
  "date": "2026-09-17",
  "updated": "2026-09-17T10:00",
  "current": {
    "temperature": 24.5,
    "feels_like": 26.0,
    "humidity": 55,
    "wind_speed": 3.3,
    "weather": "晴"
  },
  "forecast": [
    { "date": "2026-09-17", "high": 28.0, "low": 18.0, "weather": "多云" },
    { "date": "2026-09-18", "high": 29.0, "low": 19.0, "weather": "小雨" }
  ]
}
```

字段说明：`today`=地点本地今天（权威"今天"）、`date`=本次预报的起始日期、temperature=当前温度(℃)、feels_like=体感(℃)、humidity=相对湿度(%)、wind_speed=风速(m/s)、high/low=当日最高/最低气温(℃)；weather 为天气描述（由 weather_code 映射为中文）。

## weather_code → 中文映射（Open-Meteo）

- 0：晴；1：大部晴朗；2：多云；3：阴
- 45/48：雾；51/53/55：毛毛雨；56/57：冻毛毛雨
- 61/63/65：小雨/中雨/大雨；66/67：冻雨
- 71/73/75：小雪/中雪/大雪；77：米雪
- 80/81/82：阵雨；85/86：阵雪
- 95：雷暴；96/99：雷暴伴冰雹

## 说明

- 免费 API（Open-Meteo），无需 Key
- 天气查询结果一律以 JSON 返回；用户需要时再转换为表格或文本
- 自检：`python3 scripts/weather.py --selftest`（离线校验核心逻辑）

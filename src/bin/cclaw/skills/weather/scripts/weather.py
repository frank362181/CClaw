#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
天气预报工具（get_weather 脚本实现）

约定（与 cclawservice/harness/scriptrunner/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"city": "杭州市"}（可选 date / day_offset）
  - 结果 JSON 写到 stdout（ensure_ascii=False），退出码 0 表示成功
  - 失败时退出码非 0，stdout 为 {"error": "..."}，stderr 为详细错误

特性：
  - 城市名兼容行政后缀（杭州市→杭州），原始名匹配不到自动剥离再试
  - 日期寻址：不给日期 = 今天起 3 天；date=YYYY-MM-DD 或 day_offset=0/1/2（今天/明天/后天）
  - 输出顶层 today = 地点本地日期（权威"今天"，避免用机器时钟猜日期）
  - 免费 Open-Meteo API，无需 Key
  - 离线自检：python3 scripts/weather.py --selftest
"""

import datetime
import json
import os
import ssl
import sys
import urllib.parse
import urllib.request

GEOCODE_URL = "https://geocoding-api.open-meteo.com/v1/search"
FORECAST_URL = "https://api.open-meteo.com/v1/forecast"
HTTP_TIMEOUT = 8  # 秒

FORECAST_WINDOW_DAYS = 7  # 向 API 拉取的窗口（决定 date/day_offset 的可选范围）
FORECAST_RETURN_DAYS = 3  # 默认返回的天数（锚点起 3 天）

# 各平台常见 CA 证书路径(macOS Python 常缺少默认 CA,需要兜底)
CERT_CANDIDATES = [
    "/etc/ssl/cert.pem",                   # macOS 系统
    "/usr/local/etc/openssl/cert.pem",     # Homebrew OpenSSL
    "/etc/pki/tls/certs/ca-bundle.crt",    # RHEL/CentOS
    "/etc/ssl/certs/ca-certificates.crt",  # Debian/Ubuntu
]

_opener = None


def _get_opener():
    global _opener
    if _opener is None:
        ctx = ssl.create_default_context()
        for cafile in CERT_CANDIDATES:
            if os.path.exists(cafile):
                try:
                    ctx.load_verify_locations(cafile)
                    break
                except Exception:
                    continue
        _opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx))
    return _opener


def http_get_json(url):
    req = urllib.request.Request(url, headers={"User-Agent": "cclaw-weather/1.0"})
    with _get_opener().open(req, timeout=HTTP_TIMEOUT) as resp:
        return json.loads(resp.read().decode("utf-8"))


# 常见行政区划后缀（长→短，优先剥离更长后缀）
# 注："岛" 故意不列入（"秦皇岛"会误判成"秦皇"），只列无歧义的 "群岛"。
SUFFIXES = ["自治州", "群岛", "地区", "市辖区", "自治区", "盟", "市", "县", "区", "州", "镇"]

# Open-Meteo weather_code → 中文描述
WEATHER_CN = {
    0: "晴", 1: "大部晴朗", 2: "多云", 3: "阴",
    45: "雾", 48: "雾",
    51: "毛毛雨", 53: "毛毛雨", 55: "毛毛雨",
    56: "冻毛毛雨", 57: "冻毛毛雨",
    61: "小雨", 63: "中雨", 65: "大雨",
    66: "冻雨", 67: "冻雨",
    71: "小雪", 73: "中雪", 75: "大雪", 77: "米雪",
    80: "阵雨", 81: "阵雨", 82: "阵雨",
    85: "阵雪", 86: "阵雪",
    95: "雷暴", 96: "雷暴伴冰雹", 99: "雷暴伴冰雹",
}


def strip_suffix(city):
    """去除常见行政区划后缀；剥离后过短（≤1 字）或无后缀时返回原样。"""
    for sfx in SUFFIXES:
        if city.endswith(sfx) and len(city) > len(sfx) + 1:
            return city[:-len(sfx)]
    return city


def candidates(city):
    """候选城市名，按命中概率排序：

    1. 原始名
    2. 剥离行政后缀名（杭州市→杭州）
    3. 补"市"名 —— 单名城市在 Open-Meteo 地理库里常常**只以带后缀的形式**存在
       （实测："温州" 查不到，"温州市" 命中），不补就会把"查不到"误报成"没有这个城市"。
    """
    out = []
    for name in (city, strip_suffix(city)):
        if name and name not in out:
            out.append(name)
    if not city.endswith("市") and 2 <= len(city) <= 4:
        filled = city + "市"
        if filled not in out:
            out.append(filled)
    return out


def geocode(city):
    """城市名 → (纬度, 经度)；多候选逐个尝试，命中中优先取中国境内结果。

    取 count=5 而非 1：同名城市在其它国家也存在（如 Suzhou/苏州），
    多取几条再按 country_code 优先中国，避免把境外同名地当成目标城市。
    """
    for name in candidates(city):
        params = urllib.parse.urlencode(
            {"name": name, "count": 5, "language": "zh", "format": "json"}
        )
        data = http_get_json(GEOCODE_URL + "?" + params)
        results = data.get("results") or []
        if not results:
            continue
        picked = results[0]
        for item in results:
            if str(item.get("country_code", "")).upper() == "CN":
                picked = item
                break
        return picked["latitude"], picked["longitude"]
    raise ValueError("city not found: %s" % city)


def weather_cn(code):
    """weather_code → 中文描述；None（上游未返回）降级为"未知"，不抛异常。"""
    if code is None:
        return "未知"
    return WEATHER_CN.get(code, "代码%d" % code)


def _daily_safe(daily, key, i):
    arr = daily.get(key) or []
    return arr[i] if i < len(arr) else None


def parse_day(text):
    """解析 YYYY-MM-DD；非法返回 None。"""
    try:
        return datetime.date.fromisoformat(str(text).strip())
    except (TypeError, ValueError):
        return None


def resolve_anchor(days, date_arg=None, offset_arg=None):
    """把用户给的日期/天数偏移解析为 days（API 返回的日期窗口）中的下标。

    - 都不传：0（今天）
    - day_offset：0=今天、1=明天…（越界报错）
    - date："YYYY-MM-DD"，必须落在窗口内
    非法输入抛 ValueError，错误信息里带可用区间（便于模型自我纠正后重试），
    避免不报错就默认回今天，让调用方拿到错误日期。
    """
    has_date = date_arg not in (None, "")
    has_offset = offset_arg not in (None, "")
    if has_date and has_offset:
        raise ValueError("date 与 day_offset 只能二选一")
    if not days:
        raise ValueError("forecast window is empty")

    if has_date:
        target = parse_day(date_arg)
        if target is None:
            raise ValueError("date 格式必须是 YYYY-MM-DD: %s" % date_arg)
        for i, day in enumerate(days):
            if parse_day(day) == target:
                return i
        raise ValueError("date 超出可用范围 %s..%s: %s" % (days[0], days[-1], date_arg))

    if has_offset:
        try:
            idx = int(offset_arg)
        except (TypeError, ValueError):
            raise ValueError("day_offset 必须是整数 0..%d" % (len(days) - 1))
        if idx < 0 or idx >= len(days):
            raise ValueError("day_offset 超出可用范围 0..%d: %s" % (len(days) - 1, offset_arg))
        return idx

    return 0


def fetch_window(lat, lon):
    """拉取 FORECAST_WINDOW_DAYS 天的原始预报，返回 (current, daily, days)。

    timezone=auto ⇒ days 为**地点本地日期**，days[0] 即为该地点的"今天"
    （不用服务端时钟猜日期，避免时区/跨日偏差）。
    """
    params = urllib.parse.urlencode({
        "latitude": "%.6f" % lat,
        "longitude": "%.6f" % lon,
        "current": "temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m",
        "daily": "weather_code,temperature_2m_max,temperature_2m_min",
        "timezone": "auto",
        "forecast_days": str(FORECAST_WINDOW_DAYS),
    })
    data = http_get_json(FORECAST_URL + "?" + params)
    daily = data.get("daily", {})
    return data.get("current", {}), daily, (daily.get("time") or [])


def build_forecast(city, cur, daily, days, anchor):
    """按锚点下标组装结构化结果（锚点起 FORECAST_RETURN_DAYS 天）。"""
    if not days:
        raise ValueError("forecast window is empty")

    start = anchor if 0 <= anchor < len(days) else 0
    end = min(start + FORECAST_RETURN_DAYS, len(days))

    forecast_list = []
    for i in range(start, end):
        forecast_list.append({
            "date": days[i],
            "high": _daily_safe(daily, "temperature_2m_max", i),
            "low": _daily_safe(daily, "temperature_2m_min", i),
            "weather": weather_cn(_daily_safe(daily, "weather_code", i) or 0),
        })

    return {
        "city": city,
        "today": days[0],   # 地点本地日期（权威"今天"，供模型校对相对日期）
        "date": days[start],  # 本次预报的起始日期
        "updated": cur.get("time", ""),
        "current": {
            "temperature": cur.get("temperature_2m"),
            "feels_like": cur.get("apparent_temperature"),
            "humidity": cur.get("relative_humidity_2m"),
            "wind_speed": cur.get("wind_speed_10m"),
            "weather": weather_cn(cur.get("weather_code")),
        },
        "forecast": forecast_list,
    }


def fetch_forecast(city, lat, lon, anchor=0):
    """一步到位：拉窗口 + 按锚点组装（anchor=0 即今天起 3 天）。"""
    cur, daily, days = fetch_window(lat, lon)
    return build_forecast(city, cur, daily, days, anchor)


def main():
    raw = sys.stdin.read() or "{}"
    try:
        args = json.loads(raw)
    except json.JSONDecodeError:
        print(json.dumps({"error": "invalid args json"}, ensure_ascii=False))
        sys.exit(2)

    city = str(args.get("city") or "").strip()
    if not city:
        print(json.dumps({"error": "city is empty"}, ensure_ascii=False))
        sys.exit(2)

    try:
        lat, lon = geocode(city)
        cur, daily, days = fetch_window(lat, lon)
        anchor = resolve_anchor(days, args.get("date"), args.get("day_offset"))
        result = build_forecast(city, cur, daily, days, anchor)
        print(json.dumps(result, ensure_ascii=False))
    except Exception as exc:  # 网络/解析/参数失败：stdout 输出错误 JSON，退出码非 0
        print(json.dumps({"error": str(exc)}, ensure_ascii=False))
        sys.exit(1)


def selftest():
    assert strip_suffix("杭州市") == "杭州"
    assert strip_suffix("北京市") == "北京"
    assert strip_suffix("凉山自治州") == "凉山"
    assert strip_suffix("沙市") == "沙市"  # 剥离后过短，保留原样
    assert strip_suffix("上海") == "上海"
    assert candidates("杭州市") == ["杭州市", "杭州"]
    assert candidates("杭州") == ["杭州", "杭州市"]
    assert candidates("温州") == ["温州", "温州市"]  # 不补后缀会查不到
    assert candidates("Wenzhou") == ["Wenzhou"]
    assert strip_suffix("舟山群岛") == "舟山"
    assert strip_suffix("秦皇岛") == "秦皇岛"  # "岛"不剥离，防误判
    assert weather_cn(0) == "晴"
    assert weather_cn(61) == "小雨"
    assert weather_cn(999) == "代码999"
    assert weather_cn(None) == "未知"  # 上游未返回当前天气码时不得崩

    # 日期寻址：不传→今天；day_offset→偏移；date→精确匹配；非法输入必须报错
    days = ["2026-09-17", "2026-09-18", "2026-09-19", "2026-09-20"]
    assert resolve_anchor(days) == 0
    assert resolve_anchor(days, None, 0) == 0
    assert resolve_anchor(days, None, 1) == 1
    assert resolve_anchor(days, None, "2") == 2
    assert resolve_anchor(days, "2026-09-19") == 2
    for bad in [("2026-09-16", None), ("2026-9-19", None), (None, 9), (None, "x"),
                ("2026-09-18", 1), (None, -1)]:
        try:
            resolve_anchor(days, *bad)
        except ValueError:
            continue
        raise AssertionError("resolve_anchor 应当报错: %r" % (bad,))

    # 组装：today/date 回带 + 锚点起 3 天（最后一天自动缩短）
    fake_daily = {
        "time": days,
        "weather_code": [0, 61, 3, 45],
        "temperature_2m_max": [30.0, 29.0, 28.0, 27.0],
        "temperature_2m_min": [20.0, 19.0, 18.0, 17.0],
    }
    fake_cur = {"time": "2026-09-17T23:00", "temperature_2m": 25.4}
    out = build_forecast("杭州", fake_cur, fake_daily, days, 1)
    assert out["today"] == "2026-09-17" and out["date"] == "2026-09-18"
    assert [d["date"] for d in out["forecast"]] == ["2026-09-18", "2026-09-19", "2026-09-20"]
    assert out["forecast"][0]["weather"] == "小雨"
    assert len(build_forecast("杭州", fake_cur, fake_daily, days, 3)["forecast"]) == 1
    assert build_forecast("杭州", fake_cur, fake_daily, days, 0)["forecast"][0]["date"] == "2026-09-17"
    print("selftest ok")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        main()

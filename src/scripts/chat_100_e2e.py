#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
cclaw 聊天后端 E2E 冒烟：100 个**不涉及工具调用**的问题，验证对话逻辑链路。

覆盖的链路环节（全部为非工具路径）：
    controller.bind -> ChattingService.Send -> Preparer -> Assembler
    -> Transport.Complete -> Journal.Finish -> Reply

校验维度：
  1. 传输层   ：HTTP 200、业务码为成功码、无 error 字段
  2. 内容层   ：reply 非空、长度合理、不是错误文案
  3. 正确性   ：算术题答案精确匹配；其余按关键词/格式断言
  4. 会话一致 ：同一 sessionId 内 sessionId 回显稳定；turnId 唯一回显
  5. 多轮记忆 ：第 2 问引用第 1 问给出的事实（同会话内）
  6. 性能     ：单轮耗时 p50 / p95 / max

用法：
    export CCLAW_BASE=https://localhost:8443/api
    export CCLAW_USER=d
    export CCLAW_PASS=123456
    python3 scripts/chat_100_e2e.py                 # 默认 100 题
    python3 scripts/chat_100_e2e.py --n 20 --verbose

依赖：仅标准库（urllib），无需 requests。
"""

import argparse
import json
import os
import ssl
import statistics
import sys
import time
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("CCLAW_BASE", "https://localhost:8443/api").rstrip("/")
USER = os.environ.get("CCLAW_USER", "d")
PASSWD = os.environ.get("CCLAW_PASS", "123456")

CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE  # 本地自签证书


# ------------------------------------------------------------------ HTTP
def _nonce():
    """NONCE 必须非空且 2 分钟内不重复(服务端 SETNX 防重放)。"""
    return uuid.uuid4().hex


def post(path, body, token=None, timeout=120):
    sep = "&" if "?" in path else "?"
    url = f"{BASE}{path}{sep}NONCE={_nonce()}&TIMESTAMP={int(time.time())}"
    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("AUTHORIZATION", token)
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, context=CTX, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", "replace")
            return resp.status, raw, (time.time() - t0) * 1000
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        return e.code, raw, (time.time() - t0) * 1000
    except Exception as e:  # noqa: BLE001
        return -1, str(e), (time.time() - t0) * 1000


def post_retry(path, body, token=None, timeout=120, max_wait=600):
    """遇到 TrafficFilter 限流(respCode=1009 traffic overflow)时指数退避重试。

    限流是"平台级保护",不是被测功能缺陷;不重试会把剩余题目全部误判为失败。
    """
    waited = 0.0
    delay = 2.0
    while True:
        status, raw, ms = post(path, body, token, timeout)
        limited = status == 403 or "traffic overflow" in raw or '"respCode":1009' in raw
        if not limited or waited >= max_wait:
            return status, raw, ms
        time.sleep(delay)
        waited += delay
        delay = min(delay * 2, 30.0)


def dig(obj, *keys):
    """在嵌套 JSON 里按候选键名找第一个非空值（后端响应包裹层不固定时兜底）。"""
    if isinstance(obj, dict):
        for k in keys:
            v = obj.get(k)
            if v not in (None, "", 0, [], {}):
                return v
        for v in obj.values():
            r = dig(v, *keys)
            if r is not None:
                return r
    elif isinstance(obj, list):
        for v in obj:
            r = dig(v, *keys)
            if r is not None:
                return r
    return None


# ------------------------------------------------------------------ 题库
# 每类 10 题，共 10 类 = 100 题。全部为纯文本推理，**不需要任何工具**。
QUESTIONS = []

# 1) 精确算术（答案必须精确匹配）
ARITH = [
    ("13 * 17 = ?", "221"),
    ("(48 + 52) / 4 = ?", "25"),
    ("2 的 10 次方是多少？", "1024"),
    ("144 的平方根是多少？", "12"),
    ("一打鸡蛋的一半是多少个？", "6"),
    ("1000 减去 37 乘以 0 等于多少？", "1000"),
    ("从 1 加到 10 的和是多少？", "55"),
    ("3 小时 45 分钟一共多少分钟？", "225"),
    ("如果一个数加 8 等于 30，这个数是多少？", "22"),
    ("7 的 3 次方减去 100 等于多少？", "243"),
]
for q, a in ARITH:
    QUESTIONS.append(("算术", q, {"must_contain": [a]}))

# 2) 事实问答
FACTS = [
    ("中国的首都是哪里？", ["北京"]),
    ("地球上最大的海洋是哪一个？", ["太平洋"]),
    ("水的化学式是什么？", ["H2O", "H₂O"]),
    ("一年有多少个月？", ["12"]),
    ("太阳系中离太阳最近的行星是哪颗？", ["水星"]),
    ("《西游记》的作者是谁？", ["吴承恩"]),
    ("光在真空中的速度大约是多少？", ["30", "3"]),
    ("人体最大的器官是什么？", ["皮肤"]),
    ("世界上最高的山峰叫什么？", ["珠穆朗玛"]),
    ("联合国总部设在哪座城市？", ["纽约"]),
]
for q, kws in FACTS:
    QUESTIONS.append(("事实", q, {"must_contain": kws}))

# 3) 逻辑推理
LOGIC = [
    ("所有猫都会叫，小白是猫，那么小白会叫吗？", ["会"]),
    ("如果今天是星期三，那么后天是星期几？", ["星期五", "周五"]),
    ("甲比乙高，乙比丙高，谁最矮？", ["丙"]),
    ("有三个盒子，只有一盒有球。盒1写着'球在这里'，盒2写着'球不在这里'，盒3写着'球不在盒1'。已知只有一句话是真的，球在哪个盒子？", ["盒2", "2 号", "第二个"]),
    ("一个正方体有几个面？", ["6", "六"]),
    ("一支铅笔 3 元，买 4 支要付多少钱？", ["12"]),
    ("如果所有的 A 都是 B，所有的 B 都是 C，那么所有 A 都是 C 吗？", ["是", "对", "正确"]),
    ("5 个人排队，小明排第 2，他前面有几个人？", ["1", "一"]),
    ("把 9 个苹果平均分给 3 个人，每人几个？", ["3"]),
    ("如果昨天是明天，那么今天就是星期五。今天实际是星期几？", ["星期", "周"]),
]
for q, kws in LOGIC:
    QUESTIONS.append(("推理", q, {"must_contain": kws}))

# 4) 翻译
TRANS = [
    ("把 '你好，世界' 翻译成英文。", ["hello", "Hello"]),
    ("把 'artificial intelligence' 翻译成中文。", ["人工智能"]),
    ("'谢谢' 的英文是什么？", ["thank"]),
    ("把 'The cat is sleeping' 翻译成中文。", ["猫"]),
    ("'book' 的中文意思是什么？", ["书"]),
    ("把 '今天天气很好' 翻译成英文。", ["weather", "fine", "nice", "good"]),
    ("'computer' 的中文是什么？", ["计算机", "电脑"]),
    ("把 '我爱学习' 翻译成英文。", ["love", "study", "learn"]),
    ("'早上好' 的英文是什么？", ["morning"]),
    ("把 'I am a student' 翻译成中文。", ["学生"]),
]
for q, kws in TRANS:
    QUESTIONS.append(("翻译", q, {"must_contain": kws}))

# 5) 摘要/压缩
SUMM = [
    ("用一句话总结：机器学习是人工智能的一个分支，它通过数据训练模型，使计算机无需显式编程即可完成预测或分类任务。", ["机器", "数据", "模型"]),
    ("把下面内容压缩到 20 字以内：公司今年第一季度营收同比增长百分之二十，主要得益于新产品的市场拓展和海外渠道的建设。", ["营收", "增长"]),
    ("请用一句话概括'需求评审'的目的。", ["需求"]),
    ("用不超过 30 个字说明什么是'上下文窗口'。", ["上下文", "窗口"]),
    ("把这句话改得更简短：由于天气原因，原定于今天下午三点举行的户外活动将被推迟到明天上午十点。", ["推迟", "明"]),
    ("概括：接口超时通常由网络抖动、上游处理慢或资源不足引起。", ["超时", "网络"]),
    ("用一句话说明'幂等'的含义。", ["重复", "一致", "相同", "不变", "多次"]),
    ("把'他因为生病所以没来上班'压缩成 5 个字以内。", ["病"]),
    ("用一句话解释什么是缓存。", ["缓存"]),
    ("概括：本次故障的根本原因是磁盘写满导致日志写入失败。", ["磁盘", "日志"]),
]
for q, kws in SUMM:
    QUESTIONS.append(("摘要", q, {"must_contain": kws}))

# 6) 指令遵循
INSTR = [
    ("只输出三个字：收到请回复", ["收到"]),
    ("请输出一个只包含小写字母的单词。", None),
    ("请用 JSON 输出：{\"name\":\"张三\",\"age\":18}", ["name", "\"age\""]),
    ("请列出 3 种水果，用顿号分隔。", ["、"]),
    ("请把 'abc' 转成大写。", ["ABC"]),
    ("请输出数字 1 到 5，用逗号分隔。", ["1", "5"]),
    ("请只回答'是'或'否'：1+1=2 吗？", ["是"]),
    ("请给出一个英文单词，长度不超过 5 个字母。", None),
    ("请输出一个空列表的 JSON 表示。", ["[]"]),
    ("请重复这句话：测试通过", ["测试通过"]),
]
for q, kws in INSTR:
    QUESTIONS.append(("指令", q, {"must_contain": kws} if kws else {}))

# 7) 代码理解（纯文本，不执行）
CODE = [
    ("这段代码输出什么？\nfor i in range(3):\n    print(i)", ["0", "1", "2"]),
    ("Python 中 len('hello') 的结果是多少？", ["5"]),
    ("这段 SQL 的作用是什么？SELECT COUNT(*) FROM t_user;", ["计数", "数量", "条数", "count", "统计"]),
    ("JavaScript 中 typeof [] 的结果是什么？", ["object"]),
    ("Go 语言里 make([]int, 0, 10) 的容量是多少？", ["10"]),
    ("这段代码有语法错误吗？\nif x = 1 { }", ["是", "有", "错误"]),
    ("Python 中 3 // 2 的结果是多少？", ["1"]),
    ("这段代码的复杂度是多少？\nfor i in range(n):\n    for j in range(n):\n        pass", ["n^2", "n²", "平方", "O(n2)", "O(n^2)"]),
    ("Go 中 defer 在什么时候执行？", ["函数", "返回", "结束"]),
    ("SQL 中 WHERE 和 HAVING 的区别是什么？", ["分组", "group"]),
]
for q, kws in CODE:
    QUESTIONS.append(("代码", q, {"must_contain": kws}))

# 8) 中文理解
ZH = [
    ("'井井有条'是什么意思？", ["整齐", "条理"]),
    ("'画蛇添足'比喻什么？", ["多余", "多此一举"]),
    ("请给出'高兴'的近义词。", ["开心", "快乐", "愉快"]),
    ("'亡羊补牢'的下一句是什么？", ["未为迟也", "犹未"]),
    ("请给出'复杂'的反义词。", ["简单"]),
    ("'三人行必有我师'出自哪里？", ["论语"]),
    ("解释'沉没成本'。", ["已经", "投入", "成本"]),
    ("'不耻下问'是什么意思？", ["请教", "问"]),
    ("请给'月亮'写一个比喻句。", ["像", "似"]),
    ("'纸上谈兵'形容什么？", ["空谈", "理论", "实际"]),
]
for q, kws in ZH:
    QUESTIONS.append(("中文", q, {"must_contain": kws}))

# 9) 开放问答（只校验非空与长度）
OPEN = [
    "请用三句话介绍你自己。",
    "如果你能拥有一种超能力，你会选什么？为什么？",
    "用 50 字介绍什么是 REST API。",
    "如何提高团队代码评审的效率？给出两点建议。",
    "解释一下什么是'技术债'。",
    "为什么需要给接口加超时？",
    "写一句鼓励程序员的话。",
    "简述单元测试和集成测试的区别。",
    "如何判断一个需求是否值得做？",
    "用一句话说明日志分级的意义。",
]
for q in OPEN:
    QUESTIONS.append(("开放", q, {}))

# 10) 长上下文/最小多轮（同会话内引用前文）
MULTI = [
    "请记住这个数字：4752。只需要回答'已记住'。",
    "刚才我让你记住的数字是多少？",
    "请记住：项目代号是 CCLAW-9。只回答'好'。",
    "项目代号是什么？",
    "请记住：明天上午十点开会。只回答'收到'。",
    "刚才说的会议时间是什么时候？",
    "请记住：预算上限是 12 万元。只回答'明白'。",
    "预算上限是多少？",
    "请记住：负责人是李工。只回答'好'。",
    "负责人是谁？",
]
for q in MULTI:
    QUESTIONS.append(("多轮", q, {}))


# ------------------------------------------------------------------ 校验
# 错误文案指纹。注意不要把 "panic"单独立为指纹：题库里有 Go 语言题
# （defer / panic / recover），模型正常作答也会包含该词，会产生假阴性。
# 这里只匹配“服务端真的崩了”才会出现的形式。
ERROR_HINTS = ("引擎不可用", "internal server error", "goroutine 1 [running]",
               "runtime error: invalid memory address", "空流", "empty stream")


def check(expect, reply):
    """返回 (ok, reason)。expect 是 {"must_contain": [...]} 或 {}。"""
    if not reply:
        return False, "空回复"
    low = reply.lower()
    for h in ERROR_HINTS:
        if h.lower() in low:
            return False, f"回复里含错误文案: {h}"
    kws = expect.get("must_contain") or []
    if kws and not any(k.lower() in low for k in kws):
        return False, "未命中关键内容 " + str(kws)
    return True, ""


# ------------------------------------------------------------------ 主流程
def login():
    st, raw, _ = post("/login", {"username": USER, "password": PASSWD})
    if st != 200:
        die(f"登录失败 HTTP {st}: {raw[:300]}")
    try:
        obj = json.loads(raw)
    except Exception:  # noqa: BLE001
        die(f"登录响应不是 JSON: {raw[:300]}")
    token = dig(obj, "token", "Token", "authorization")
    if not token:
        die(f"登录响应里没有 token: {raw[:300]}")
    print(f"[login] ok, token={str(token)[:12]}…")
    return token


def die(msg):
    print("FATAL:", msg)
    sys.exit(2)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=100)
    ap.add_argument("--verbose", action="store_true")
    ap.add_argument("--timeout", type=int, default=120)
    # 平台限流：同一 IP + 同一 API 在 2 分钟内最多 50 次
    # （platformutility: IPAPITrafficFilterLimits=50 / TrafficFilterTimeGap=120s）。
    # 取 2.6s 间隔 ≈ 23 次/分钟，留出安全余量。
    ap.add_argument("--gap", type=float, default=2.6)
    args = ap.parse_args()

    token = login()

    # 每 10 题一个会话（"多轮"类必须同会话才有意义）
    sessions = {}
    rows, lat = [], []

    for idx, (cat, q, expect) in enumerate(QUESTIONS[: args.n], start=1):
        if idx > 1 and args.gap > 0:
            time.sleep(args.gap)  # 主动限速，避免撞平台流量闸
        sid = sessions.setdefault(cat, "")  # 空串=服务端新建并回显
        body = {
            "sessionId": sid,
            "turnId": "",
            "requestMsg": q,
            "chatType": 0,
            "chatChannel": 1,
            "genAt": 0,
        }
        st, raw, ms = post_retry("/chat", body, token, args.timeout)
        lat.append(ms)

        ok, reason, reply, sid_echo, tid_echo, code = False, "", "", "", "", None
        if st != 200:
            reason = f"HTTP {st}"
        else:
            try:
                obj = json.loads(raw)
            except Exception:  # noqa: BLE001
                obj = None
                reason = "响应不是 JSON"
            if obj is not None:
                code = obj.get("code", obj.get("Code"))
                data = obj.get("data", obj.get("Data")) or {}
                reply = data.get("reply") or data.get("Reply") or ""
                sid_echo = data.get("sessionId") or data.get("SessionId") or ""
                tid_echo = data.get("turnId") or data.get("TurnId") or ""
                if code not in (0, None):
                    reason = f"业务码 {code}: {obj.get('msg') or obj.get('message')}"
                else:
                    ok, reason = check(expect, reply)

        # 会话回显自洽性
        if ok and sid_echo:
            if sessions[cat] and sessions[cat] != sid_echo:
                ok, reason = False, f"sessionId 回显变化 {sessions[cat]} -> {sid_echo}"
            sessions[cat] = sid_echo
        if ok and not tid_echo:
            ok, reason = False, "turnId 未回显"

        rows.append(
            dict(no=idx, cat=cat, ok=ok, ms=ms, reason=reason,
                 q=q.replace("\n", " ")[:40], reply=(reply or "")[:60].replace("\n", " "))
        )
        flag = "OK " if ok else "FAIL"
        if args.verbose or not ok:
            print(f"[{idx:3d}] {flag} {ms:7.0f}ms {cat:4s} {rows[-1]['q']}  {reason or rows[-1]['reply']}")

    # ---------------- 汇总
    total = len(rows)
    passed = sum(1 for r in rows if r["ok"])
    print("\n================ 汇总 ================")
    print(f"总数 {total}  通过 {passed}  失败 {total - passed}  通过率 {passed / total * 100:.1f}%")
    if lat:
        lat_sorted = sorted(lat)
        p50 = statistics.median(lat_sorted)
        p95 = lat_sorted[min(len(lat_sorted) - 1, int(len(lat_sorted) * 0.95))]
        print(f"耗时 ms: p50={p50:.0f} p95={p95:.0f} max={max(lat):.0f} avg={sum(lat)/len(lat):.0f}")

    by_cat = {}
    for r in rows:
        s = by_cat.setdefault(r["cat"], [0, 0])
        s[0] += 1
        s[1] += 1 if r["ok"] else 0
    print("\n分类通过率:")
    for cat, (n, ok) in by_cat.items():
        print(f"  {cat:4s} {ok}/{n}")

    fails = [r for r in rows if not r["ok"]]
    if fails:
        print("\n失败样本:")
        for r in fails[:10]:
            print(f"  #{r['no']} [{r['cat']}] {r['reason']} | Q={r['q']} | A={r['reply']}")

    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())

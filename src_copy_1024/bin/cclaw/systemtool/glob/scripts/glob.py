#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
glob.py —— 系统工具 Glob 的脚本实现（bin/cclaw/systemtool/glob）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"pattern": "**/*.py", "path": "src", "case_insensitive": false, "max_results": 200}
  - 结果（文本）写 stdout，退出码 0 = 成功；失败退出码非 0（stderr 为可读错误，stdout 为 {"error":...}）

与 glob.json 声明的语义一一对应：
  - 按**文件名/路径模式**查找文件，只返回文件路径（不打开文件内容；搜内容请用 Grep）
  - pattern 支持 * ? [abc] ** 与 {a,b} 花括号；** 表示任意层级（**/ 可匹配零层）
  - pattern 不含 "/" 时按**文件名**匹配（任意深度都能命中）；含 "/" 时按**相对路径**匹配
  - path 支持绝对路径、~/ 路径、工作区相对路径；可以是目录，也可以是单个文件
  - 结果按**修改时间倒序**（最新在前），同时间按路径升序，保证输出稳定可复现
  - 跳过噪声目录（.git / node_modules / __pycache__ 等）与敏感文件名（.env / *.pem / *.key 等）
  - 只列出**普通文件**：目录、管道、设备、socket 不会出现在结果中

输出预算：execbox 的 stdout 上限为 256KB，本脚本自带 200KB 预算，超预算主动收尾并提示收窄条件。
所有错误提示与输出注记均为中文，便于模型直接理解并自我纠正。

自检：python3 scripts/glob.py --selftest
"""

import json
import os
import re
import sys

# ⚠️ 本脚本名为 glob.py，会遮蔽标准库 glob。Python 3.13+ 的 pathlib 内部有
#    `from glob import _StringGlobber`，若不处理会直接报
#    ImportError: cannot import name 'Path' ... (most likely due to a circular import)。
# 因此必须在导入 pathlib 之前把「脚本自身目录」从 sys.path 里摘掉。
# 本脚本不依赖同目录下的其它模块，摘掉是安全的；新增依赖请同时确认不会因此失效。
_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
for _idx, _entry in enumerate(sys.path):
    if _entry and os.path.abspath(_entry) == _SCRIPT_DIR:
        sys.path.pop(_idx)
        break

from pathlib import Path

TOOL_NAME = "glob"
VERSION = "1.0.0"

DEFAULT_MAX_RESULTS = 200         # 默认返回条数上限（glob.json）
MAX_SCAN_MATCHES = 5000           # 扫描期最多收集的匹配数（按 mtime 排序需要全量收集，此处兜底内存）
MAX_OUTPUT_BYTES = 200 * 1024     # 自身输出预算，需小于 execbox 的 256KB

# 遍历时直接剪枝的噪声目录（避免在依赖目录/版本库上耗尽一次调用）
_SKIP_DIRS = {
    ".git", ".hg", ".svn",
    "node_modules", "__pycache__",
    ".venv", "venv", ".tox",
    ".mypy_cache", ".pytest_cache", ".ruff_cache",
    ".idea", ".vscode",
}

# 敏感文件名：只按「文件名 / 后缀」判定（glob 只列名字，不做全路径关键词匹配，
# 否则像 .../credentials/... 这样的目录会整棵子树被误杀）
_SENSITIVE_NAMES = {".env", ".netrc", ".pgpass", "id_rsa", "id_ecdsa", "credentials", "known_hosts"}
_SENSITIVE_SUFFIX = (".pem", ".key", ".p12", ".pfx", ".db", ".sqlite", ".sqlite3", ".crt")


# --------------------------------------------------------------------------
# 路径解析（与 read/grep 同口径：绝对路径 / ~ / 工作区相对路径）
# --------------------------------------------------------------------------

def _find_bin_root():
    """从脚本位置向上找第一个名为 bin 的目录（服务可执行目录）。"""
    script = Path(__file__).resolve()
    for parent in script.parents:
        if parent.name == "bin":
            return parent
    return script.parent


def _workspace_root():
    """数据/产物工作区根目录：<bin>/workspace。"""
    return _find_bin_root() / "workspace"


def _agent_workspace_root():
    """agent 工作区根：优先取服务端注入的 CCLAW_AGENT_WORKSPACE，缺省回退 <bin>/workspace。"""
    env = (os.environ.get("CCLAW_AGENT_WORKSPACE") or "").strip()
    return Path(env) if env else _workspace_root()


def resolve_search_root(raw):
    """把 path 解析为存在的真实路径（文件或目录）；为空时用 agent 工作区。

    支持绝对路径、~/ 路径、工作区相对路径；不限制必须位于工作区内。
    """
    text = str(raw or "").strip()

    if text == "":
        root = _agent_workspace_root()
        try:
            return root.resolve(strict=True)
        except OSError:
            raise ValueError("工作区不存在：%s" % root)

    if text.startswith("~"):
        expanded = os.path.expanduser(text)
        if expanded == text:
            raise ValueError("无法展开家目录路径：%s" % text)
        text = expanded

    hint = ""
    if os.path.isabs(text):
        candidate = Path(text)
    else:
        root = _agent_workspace_root()
        candidate = root / text
        hint = "（工作区：%s）" % root.resolve()

    try:
        return candidate.resolve(strict=True)
    except FileNotFoundError:
        raise ValueError("路径不存在：%s%s" % (text, hint))
    except OSError as exc:
        raise ValueError("无法解析路径 %s：%s" % (text, exc))


# --------------------------------------------------------------------------
# 参数
# --------------------------------------------------------------------------

def parse_int(args, key, default, minimum):
    """取整型参数（容忍 JSON 字符串/浮点整数）；非法值抛 ValueError。"""
    if key not in args or args.get(key) is None:
        return default

    value = args.get(key)
    if isinstance(value, bool):
        raise ValueError("参数 %s 必须是整数，实际收到 %r" % (key, value))
    if isinstance(value, str):
        value = value.strip()
        if value == "":
            return default
        try:
            value = int(value)
        except ValueError:
            raise ValueError("参数 %s 必须是整数，实际收到 %r" % (key, args.get(key)))
    elif isinstance(value, float):
        if value != int(value):
            raise ValueError("参数 %s 必须是整数，实际收到 %r" % (key, args.get(key)))
        value = int(value)
    elif not isinstance(value, int):
        raise ValueError("参数 %s 必须是整数，实际收到 %r" % (key, args.get(key)))

    value = int(value)
    if value < minimum:
        raise ValueError("参数 %s 必须 ≥ %d，实际收到 %d" % (key, minimum, value))
    return value


def parse_bool(args, key, default=False):
    """取布尔参数（容忍 "true"/"1"/"yes" 等字符串写法）。"""
    if key not in args or args.get(key) is None:
        return default

    value = args.get(key)
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return value != 0
    if isinstance(value, str):
        low = value.strip().lower()
        if low in ("true", "1", "yes", "y", "on"):
            return True
        if low in ("false", "0", "no", "n", "off", ""):
            return False
    raise ValueError("参数 %s 必须是布尔值，实际收到 %r" % (key, args.get(key)))


# --------------------------------------------------------------------------
# 花括号展开
# --------------------------------------------------------------------------

def _matching_brace(text, open_idx):
    """返回与 text[open_idx]（'{'）配对的闭合花括号下标；不配对返回 -1。"""
    depth = 0
    for idx in range(open_idx, len(text)):
        if text[idx] == "{":
            depth += 1
        elif text[idx] == "}":
            depth -= 1
            if depth == 0:
                return idx
    return -1


def _split_top_level(body):
    """按顶层逗号切分花括号内容（嵌套花括号内的逗号不切）。"""
    parts = []
    current = []
    depth = 0
    for ch in body:
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append("".join(current))
            current = []
            continue
        current.append(ch)
    parts.append("".join(current))
    return parts


def expand_braces(pattern):
    """展开花括号（支持嵌套）：'*.{ts,tsx}' → ['*.ts', '*.tsx']；无花括号原样返回。"""
    text = str(pattern or "").strip()
    if text == "":
        return [""]

    open_idx = text.find("{")
    if open_idx < 0:
        return [text]
    close_idx = _matching_brace(text, open_idx)
    if close_idx < 0:
        return [text]

    prefix, body, suffix = text[:open_idx], text[open_idx + 1:close_idx], text[close_idx + 1:]
    options = [item.strip() for item in _split_top_level(body) if item.strip() != ""]
    if not options:
        return [text]

    out = []
    for option in options:
        for head in expand_braces(option):
            for tail in expand_braces(suffix):
                out.append(prefix + head + tail)
    return out


# --------------------------------------------------------------------------
# glob → 正则（自己实现，保证 ** 与 * 的跨目录语义正确）
# --------------------------------------------------------------------------

def glob_to_regex(pattern):
    """把单个 glob 模式翻译成等价正则片段。

    语义对齐常见 glob 工具：
      - **/  匹配零层或多层目录（'**/*.py' 也能命中根目录下的 'a.py'）
      - **   匹配任意字符（含 /）
      - *    只匹配单层内的任意字符（不含 /）
      - ?    匹配单层内单个字符（不含 /）
      - [...]/'[!...]' 字符类原样转义后透传
    """
    i, n = 0, len(pattern)
    out = []
    while i < n:
        ch = pattern[i]

        if ch == "*":
            if pattern[i:i + 2] == "**":
                i += 2
                if i < n and pattern[i] == "/":
                    i += 1
                    out.append("(?:.*/)?")
                else:
                    out.append(".*")
                continue
            out.append("[^/]*")
            i += 1
            continue

        if ch == "?":
            out.append("[^/]")
            i += 1
            continue

        if ch == "[":
            j = i + 1
            if j < n and pattern[j] in "!^":
                j += 1
            if j < n and pattern[j] == "]":
                j += 1
            while j < n and pattern[j] != "]":
                j += 1
            if j >= n:
                # 未闭合的 '[' 按普通字符处理
                out.append(re.escape(ch))
                i += 1
                continue
            body = pattern[i + 1:j]
            if body[:1] in ("!", "^"):
                body = "^" + body[1:]
            body = body.replace("\\", "\\\\")
            out.append("[" + body + "]")
            i = j + 1
            continue

        out.append(re.escape(ch))
        i += 1

    return "".join(out)


def compile_glob(pattern, case_insensitive):
    """把 pattern（含花括号）编译成匹配函数：match(rel_path, name) -> bool。"""
    normalized = str(pattern or "").strip()
    while normalized.startswith("./"):
        normalized = normalized[2:]

    expanded = [item for item in expand_braces(normalized) if item != ""]
    if not expanded:
        raise ValueError("pattern 不能为空")

    flags = re.IGNORECASE if case_insensitive else 0
    compiled = [re.compile(r"\A(?:" + glob_to_regex(item) + r")\Z", flags) for item in expanded]

    # 所有分支都不含 "/" 时按文件名匹配（任意深度）；否则按相对路径匹配
    use_basename = all("/" not in item for item in expanded)

    def match(rel_path, name):
        target = name if use_basename else rel_path
        for regex in compiled:
            if regex.match(target):
                return True
        return False

    return match


# --------------------------------------------------------------------------
# 遍历与收集
# --------------------------------------------------------------------------

def _is_sensitive_name(name):
    low = name.lower()
    return low in _SENSITIVE_NAMES or low.endswith(_SENSITIVE_SUFFIX)


def iter_files(root):
    """遍历候选普通文件（确定性顺序）；剪枝噪声目录，不跟随符号链接进入目录。"""
    for current, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d not in _SKIP_DIRS)
        for name in sorted(files):
            path = Path(current) / name
            try:
                if not path.is_file():
                    continue
            except OSError:
                continue
            yield path


def collect(root, matcher, max_results):
    """收集匹配项并按 mtime 倒序排序，返回 (清单, 统计)。清单元素为 (相对路径, mtime)。"""
    base = root if root.is_dir() else root.parent
    candidates = [root] if root.is_file() else iter_files(root)

    entries = []
    stats = {"scanned": 0, "matched": 0, "skipped_sensitive": 0, "scan_capped": False}

    for path in candidates:
        if stats["matched"] >= MAX_SCAN_MATCHES:
            stats["scan_capped"] = True
            break

        stats["scanned"] += 1

        if _is_sensitive_name(path.name):
            stats["skipped_sensitive"] += 1
            continue

        try:
            rel = path.relative_to(base).as_posix()
        except ValueError:
            rel = path.as_posix()

        if not matcher(rel, path.name):
            continue

        try:
            mtime = path.stat().st_mtime
        except OSError:
            continue

        entries.append((rel, mtime))
        stats["matched"] += 1

    # 修改时间倒序（最新在前），同一时刻按路径升序，保证输出稳定可复现
    entries.sort(key=lambda item: (-item[1], item[0]))
    return entries[:max_results], stats


# --------------------------------------------------------------------------
# 入口
# --------------------------------------------------------------------------

def fail(message, code=1):
    """失败：stdout 给 JSON（便于直接手跑脚本核对），stderr 给可读错误（execbox 取它做错误详情）。"""
    sys.stdout.write(json.dumps({"error": message}, ensure_ascii=False) + "\n")
    sys.stderr.write(message + "\n")
    return code


def run(args):
    pattern = str(args.get("pattern") or "").strip()
    if pattern == "":
        raise ValueError("必须提供 pattern（要匹配的 glob 模式，例如 '**/*.py'）")

    if os.path.isabs(pattern):
        raise ValueError("pattern 必须是相对模式，不能是绝对路径：%s（绝对路径请放到 path 参数）" % pattern)

    max_results = parse_int(args, "max_results", DEFAULT_MAX_RESULTS, 1)
    case_insensitive = parse_bool(args, "case_insensitive", False)
    matcher = compile_glob(pattern, case_insensitive)

    root = resolve_search_root(args.get("path"))
    entries, stats = collect(root, matcher, max_results)

    lines = []
    used = 0
    listed = 0
    over_budget = False
    for rel, _mtime in entries:
        cost = len(rel.encode("utf-8")) + 1
        if lines and used + cost > MAX_OUTPUT_BYTES:
            over_budget = True
            break
        lines.append(rel)
        used += cost
        listed += 1

    base = root if root.is_dir() else root.parent
    notes = []
    if not lines:
        notes.append("（没有匹配的文件：pattern=%s，搜索目录=%s）" % (pattern, base))
    else:
        notes.append("共 %d 个文件匹配（按修改时间倒序，最新的在前）" % listed)

    if stats["matched"] > listed and not over_budget:
        # 扫描被上限截断时总数只是下界，未截断时是精确值
        total_desc = ("≥ %d" % stats["matched"]) if stats["scan_capped"] else ("%d" % stats["matched"])
        notes.append("… 结果已截断至 max_results=%d（匹配总数 %s）；请收窄 pattern 或提高 max_results"
                     % (max_results, total_desc))
    if over_budget:
        notes.append("… 输出达到 %dKB 工具输出预算并截断；请收窄 pattern 或提高过滤精度"
                     % (MAX_OUTPUT_BYTES // 1024))
    if stats["scan_capped"]:
        notes.append("… 扫描达到 %d 个匹配的上限，结果可能不完整；请收窄 pattern"
                     % MAX_SCAN_MATCHES)
    if stats["skipped_sensitive"]:
        notes.append("已跳过 %d 个敏感文件名（.env/*.pem/*.key 等）" % stats["skipped_sensitive"])

    text = "\n".join(lines)
    if notes:
        text = (text + "\n\n" if text else "") + "\n".join(notes)
    if text:
        sys.stdout.write(text + "\n")
    return 0


def main():
    raw = sys.stdin.read() or "{}"
    try:
        args = json.loads(raw)
    except json.JSONDecodeError as exc:
        return fail("参数 JSON 解析失败：%s" % exc)

    if not isinstance(args, dict):
        return fail("参数必须是 JSON 对象")

    try:
        return run(args)
    except ValueError as exc:
        return fail(str(exc))
    except OSError as exc:
        return fail("查找失败：%s" % exc)


def selftest():
    import tempfile

    # 花括号展开（含嵌套）
    assert expand_braces("*.py") == ["*.py"]
    assert expand_braces("*.{ts,tsx}") == ["*.ts", "*.tsx"]
    assert sorted(expand_braces("*.{a,{b,c}}")) == ["*.a", "*.b", "*.c"]

    # glob → 正则语义：* 不跨目录，** 跨目录，**/ 可匹配零层
    assert glob_to_regex("*.py") == r"[^/]*\.py"
    assert glob_to_regex("**/*.py") == r"(?:.*/)?[^/]*\.py"
    assert re.match(r"\A(?:" + glob_to_regex("**/*.py") + r")\Z", "a.py")
    assert re.match(r"\A(?:" + glob_to_regex("**/*.py") + r")\Z", "src/deep/a.py")
    assert not re.match(r"\A(?:" + glob_to_regex("src/*.py") + r")\Z", "src/deep/a.py")
    assert re.match(r"\A(?:" + glob_to_regex("src/*.py") + r")\Z", "src/a.py")
    assert re.match(r"\A(?:" + glob_to_regex("file?.txt") + r")\Z", "file1.txt")
    assert not re.match(r"\A(?:" + glob_to_regex("file?.txt") + r")\Z", "file12.txt")
    assert re.match(r"\A(?:" + glob_to_regex("[ab].py") + r")\Z", "a.py")
    assert not re.match(r"\A(?:" + glob_to_regex("[!ab].py") + r")\Z", "a.py")

    # 参数
    assert parse_int({}, "max_results", DEFAULT_MAX_RESULTS, 1) == DEFAULT_MAX_RESULTS
    assert parse_int({"max_results": "10"}, "max_results", 0, 1) == 10
    for bad in (-1, 0, "x", True):
        try:
            parse_int({"max_results": bad}, "max_results", DEFAULT_MAX_RESULTS, 1)
        except ValueError:
            continue
        raise AssertionError("max_results 应当报错: %r" % (bad,))
    assert parse_bool({"case_insensitive": "true"}, "case_insensitive") is True
    assert parse_bool({}, "case_insensitive") is False

    # 绝对路径 pattern 必须报错
    try:
        run({"pattern": "/tmp/*.py", "path": "."})
    except ValueError as exc:
        assert "相对模式" in str(exc), exc
    else:
        raise AssertionError("绝对路径 pattern 应当报错")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        (root / "a.py").write_text("x", encoding="utf-8")
        (root / "b.md").write_text("x", encoding="utf-8")
        (root / "UP.PY").write_text("x", encoding="utf-8")
        (root / "src").mkdir()
        (root / "src" / "c.py").write_text("x", encoding="utf-8")
        (root / "src" / "deep").mkdir()
        (root / "src" / "deep" / "d.ts").write_text("x", encoding="utf-8")
        (root / ".env").write_text("x", encoding="utf-8")
        (root / "node_modules").mkdir()
        (root / "node_modules" / "e.py").write_text("x", encoding="utf-8")

        # 不含 "/"：按文件名匹配，任意深度命中；噪声目录与敏感文件名被排除
        entries, stats = collect(root, compile_glob("*.py", False), DEFAULT_MAX_RESULTS)
        names = sorted(rel for rel, _ in entries)
        assert names == ["a.py", "src/c.py"], names
        assert stats["skipped_sensitive"] == 1, stats

        # **/*.py：跨层级
        entries, _ = collect(root, compile_glob("**/*.py", False), DEFAULT_MAX_RESULTS)
        assert sorted(rel for rel, _ in entries) == ["a.py", "src/c.py"]

        # src/*.py：只匹配单层
        entries, _ = collect(root, compile_glob("src/*.py", False), DEFAULT_MAX_RESULTS)
        assert [rel for rel, _ in entries] == ["src/c.py"]

        # 花括号
        entries, _ = collect(root, compile_glob("*.{py,md}", False), DEFAULT_MAX_RESULTS)
        assert sorted(rel for rel, _ in entries) == ["a.py", "b.md", "src/c.py"]

        # 大小写敏感/不敏感
        entries, _ = collect(root, compile_glob("*.py", False), DEFAULT_MAX_RESULTS)
        assert "UP.PY" not in [rel for rel, _ in entries]
        entries, _ = collect(root, compile_glob("*.py", True), DEFAULT_MAX_RESULTS)
        assert "UP.PY" in [rel for rel, _ in entries]

        # max_results 截断
        entries, stats = collect(root, compile_glob("*.py", True), 2)
        assert len(entries) == 2 and stats["matched"] == 3, (entries, stats)

        # 排序：修改时间倒序
        old = root / "src" / "c.py"
        os.utime(old, (1000000000, 1000000000))
        entries, _ = collect(root, compile_glob("*.py", False), DEFAULT_MAX_RESULTS)
        assert entries[0][0] == "a.py" and entries[-1][0] == "src/c.py", entries

        # 目录本身不会被当成匹配项；path 解析支持绝对路径
        assert resolve_search_root(str(root)) == root.resolve()

    print("selftest ok")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        sys.exit(main())

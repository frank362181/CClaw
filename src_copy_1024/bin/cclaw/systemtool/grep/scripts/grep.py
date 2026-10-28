#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
grep.py —— 系统工具 Grep 的脚本实现（bin/cclaw/systemtool/grep）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"pattern": "...", "path": "...", "include": "*.py",
    "case_insensitive": false, "files_only": false, "max_results": 200}
  - 结果（文本）写 stdout，退出码 0 = 成功；失败退出码非 0（stderr 为可读错误，stdout 为 {"error":...}）

与 grep.json 声明的语义一一对应：
  - 在文件**内容**里按正则搜索，只返回命中的行（文件路径 + 真实行号 + 原文）
  - pattern 支持完整正则（Python re 语法）；非法正则报可读错误
  - path 支持绝对路径、~/ 路径、工作区相对路径；可以是**目录**，也可以是**单个文件**
  - include 按文件名（或含 "/" 时按相对路径）过滤，支持 '*.py' 与 '*.{ts,tsx}' 花括号写法
  - case_insensitive 忽略大小写
  - files_only 只返回命中文件清单（附命中条数）
  - max_results 命中上限（默认 200）：超出主动收尾并提示收窄条件
  - 单行超过 2000 字符截断并就地标注
  - 跳过：二进制文件（含 NUL）、非 UTF-8 文件、超过 2MB 的文件、敏感文件（密钥/凭据/库文件）、
    常见噪声目录（.git / node_modules / __pycache__ 等）

路径解析与 read 工具保持一致：绝对路径与 ~/ 直接使用；相对路径落到 agent 工作区
（CCLAW_AGENT_WORKSPACE，缺省 <bin>/workspace）。

输出预算：execbox 的 stdout 上限为 256KB，本脚本自带 200KB 预算；超预算主动收尾并提示收窄条件
（若放任 stdout 超限，整次调用会被判失败，模型连已命中的内容都拿不到）。

实现说明：本机**不依赖 ripgrep**（rg 未必安装），完全用标准库实现，行为与语义等价。

自检：python3 scripts/grep.py --selftest
"""

import fnmatch
import json
import os
import re
import sys
from pathlib import Path

TOOL_NAME = "grep"
VERSION = "1.0.0"

DEFAULT_MAX_RESULTS = 200         # 默认命中条数上限（grep.json）
MAX_LINE_CHARS = 2000             # 单行字符上限（超过则截断并标注）
MAX_FILE_BYTES = 2 * 1024 * 1024  # 单文件扫描上限 2MB（更大的文件跳过，避免拖死一次调用）
MAX_OUTPUT_BYTES = 200 * 1024     # 自身输出预算，需小于 execbox 的 256KB
BINARY_SNIFF_BYTES = 8192         # 二进制探测读取的前缀字节数

# 扫描时直接剪枝的噪声目录（避免遍历 .git / 依赖目录导致超时）
_SKIP_DIRS = {
    ".git", ".hg", ".svn",
    "node_modules", "__pycache__",
    ".venv", "venv", ".tox",
    ".mypy_cache", ".pytest_cache", ".ruff_cache",
    ".idea", ".vscode",
}

# 与 read 工具一致：敏感文件一律不读（grep 场景下静默跳过并计数，而不是整体报错）
_SENSITIVE_NAMES = {".env", ".netrc", ".pgpass", "id_rsa", "id_ecdsa", "credentials", "known_hosts"}
_SENSITIVE_SUFFIX = (".pem", ".key", ".p12", ".pfx", ".db", ".sqlite", ".sqlite3", ".crt")
_SENSITIVE_HINT = ("password", "passwd", "secret", "credential", "signkey", "token", "privatekey")


# --------------------------------------------------------------------------
# 路径解析（与 read.py 同口径：绝对路径 / ~ / 工作区相对路径）
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


def _is_sensitive(real_path):
    name = real_path.name.lower()
    if name in _SENSITIVE_NAMES or name.endswith(_SENSITIVE_SUFFIX):
        return True

    low = str(real_path).lower()
    return any(hint in low for hint in _SENSITIVE_HINT)


def resolve_search_root(raw):
    """把 path 解析为存在的真实路径（文件或目录）；为空时用 agent 工作区。

    支持绝对路径、~/ 路径、工作区相对路径；不再限制必须位于工作区内。
    """
    text = str(raw or "").strip()

    if text == "":
        root = _agent_workspace_root()
        try:
            return root.resolve(strict=True)
        except OSError:
            raise ValueError("workspace not found: %s" % root)

    if text.startswith("~"):
        expanded = os.path.expanduser(text)
        if expanded == text:
            raise ValueError("cannot expand home directory in path: %s" % text)
        text = expanded

    hint = ""
    if os.path.isabs(text):
        candidate = Path(text)
    else:
        root = _agent_workspace_root()
        candidate = root / text
        hint = " (workspace:%s)" % root.resolve()

    try:
        return candidate.resolve(strict=True)
    except FileNotFoundError:
        raise ValueError("path not found: %s%s" % (text, hint))
    except OSError as exc:
        raise ValueError("cannot resolve path %s: %s" % (text, exc))


# --------------------------------------------------------------------------
# 参数与 include 处理
# --------------------------------------------------------------------------

def parse_int(args, key, default, minimum):
    """取整型参数（容忍 JSON 字符串/浮点整数）；非法值抛 ValueError。"""
    if key not in args or args.get(key) is None:
        return default

    value = args.get(key)
    if isinstance(value, bool):
        raise ValueError("%s must be an integer, got %r" % (key, value))
    if isinstance(value, str):
        value = value.strip()
        if value == "":
            return default
        try:
            value = int(value)
        except ValueError:
            raise ValueError("%s must be an integer, got %r" % (key, args.get(key)))
    elif isinstance(value, float):
        if value != int(value):
            raise ValueError("%s must be an integer, got %r" % (key, args.get(key)))
        value = int(value)
    elif not isinstance(value, int):
        raise ValueError("%s must be an integer, got %r" % (key, args.get(key)))

    value = int(value)
    if value < minimum:
        raise ValueError("%s must be >= %d, got %d" % (key, minimum, value))
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
    raise ValueError("%s must be a boolean, got %r" % (key, args.get(key)))


def _matching_brace(text, open_idx):
    """返回与 text[open_idx]（'}' 或 '{'）配对的闭合花括号下标；不配对返回 -1。"""
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
        # option 自身可能还带花括号（如 '*.{a,{b,c}}' 的 '{b,c}'），需一并递归展开
        for head in expand_braces(option):
            for tail in expand_braces(suffix):
                out.append(prefix + head + tail)
    return out


def compile_include(include):
    """把 include 编译成匹配函数；为空时匹配所有文件。"""
    patterns = [p for p in expand_braces(include) if p != ""]
    if not patterns:
        return lambda rel_path, name: True

    def match(rel_path, name):
        for pattern in patterns:
            target = rel_path if "/" in pattern else name
            if fnmatch.fnmatch(target, pattern):
                return True
        return False

    return match


# --------------------------------------------------------------------------
# 扫描与命中
# --------------------------------------------------------------------------

def is_binary(path):
    """按前缀是否含 NUL 判定二进制（与 read.py 同口径）。"""
    try:
        with open(path, "rb") as handle:
            return b"\x00" in handle.read(BINARY_SNIFF_BYTES)
    except OSError:
        return True


def iter_files(root, want):
    """遍历待扫描文件（确定性顺序）；目录剪枝噪声目录，不跟随符号链接。"""
    if root.is_file():
        yield root
        return

    for current, dirs, files in os.walk(root, followlinks=False):
        dirs[:] = sorted(d for d in dirs if d not in _SKIP_DIRS and not d.startswith(".git"))
        for name in sorted(files):
            path = Path(current) / name
            if not path.is_file():
                continue
            try:
                rel = path.relative_to(root).as_posix()
            except ValueError:
                rel = path.name
            if want(rel, name):
                yield path


def clip_line(text, max_chars=MAX_LINE_CHARS):
    """超长行截断并就地标注，返回 (文本, 是否截断)。"""
    if len(text) <= max_chars:
        return text, False
    return "%s …[line truncated, %d more chars]" % (text[:max_chars], len(text) - max_chars), True


def search_file(path, regex, display_path, max_hits, sink, stats):
    """在单个文件内按行搜索，把命中写入 sink；返回新增命中数。"""
    try:
        size = path.stat().st_size
    except OSError:
        stats["unreadable"] += 1
        return 0

    if size > MAX_FILE_BYTES:
        stats["oversized"] += 1
        return 0

    if is_binary(path):
        stats["binary"] += 1
        return 0

    try:
        raw = path.read_bytes()
        text = raw.decode("utf-8")
    except (OSError, UnicodeDecodeError):
        stats["undecodable"] += 1
        return 0

    added = 0
    for lineno, line in enumerate(text.splitlines(), start=1):
        if not regex.search(line):
            continue

        line, _ = clip_line(line)
        sink(display_path, lineno, line)
        added += 1
        stats["matches"] += 1
        if stats["matches"] >= max_hits:
            break
    return added


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
        raise ValueError("pattern is required")

    max_results = parse_int(args, "max_results", DEFAULT_MAX_RESULTS, 1)
    case_insensitive = parse_bool(args, "case_insensitive", False)
    files_only = parse_bool(args, "files_only", False)
    matcher = compile_include(args.get("include"))

    try:
        flags = re.IGNORECASE if case_insensitive else 0
        regex = re.compile(pattern, flags)
    except re.error as exc:
        raise ValueError("invalid regular expression %r: %s" % (pattern, exc))

    root = resolve_search_root(args.get("path"))
    # 结果里的路径：目录扫描用相对路径（更短、更好读），单文件用原样
    base = root if root.is_dir() else root.parent

    lines = []
    used = 0
    stopped_by_budget = False
    file_counts = {}
    stats = {"matches": 0, "binary": 0, "undecodable": 0, "oversized": 0, "unreadable": 0, "files": 0}

    def sink(display_path, lineno, text):
        nonlocal used, stopped_by_budget
        rendered = "%s:%d:%s" % (display_path, lineno, text)
        cost = len(rendered.encode("utf-8")) + 1
        if lines and used + cost > MAX_OUTPUT_BYTES:
            stopped_by_budget = True
            return
        lines.append(rendered)
        used += cost

    for path in iter_files(root, matcher):
        if stats["matches"] >= max_results or stopped_by_budget:
            break

        if _is_sensitive(path):
            stats["skipped_sensitive"] = stats.get("skipped_sensitive", 0) + 1
            continue

        stats["files"] += 1
        try:
            display = path.relative_to(base).as_posix()
        except ValueError:
            display = str(path)

        before = stats["matches"]
        search_file(path, regex, display, max_results, sink, stats)
        hit = stats["matches"] - before
        if hit > 0:
            file_counts[display] = hit

    notes = []
    if files_only:
        body = "\n".join("%s:%d" % (name, count) for name, count in sorted(file_counts.items()))
        total = len(file_counts)
        notes.append("%d file(s) matched, %d occurrence(s)" % (total, stats["matches"]))
    else:
        body = "\n".join(lines)
        if stats["matches"] == 0:
            notes.append("no matches for pattern %r under %s" % (pattern, base))
        else:
            notes.append("%d match(es) in %d file(s) scanned" % (stats["matches"], stats["files"]))

    if stats["matches"] >= max_results and not files_only:
        notes.append("… stopped at max_results=%d; narrow the pattern/include or raise max_results" % max_results)
    if stopped_by_budget:
        notes.append("… output truncated at the %dKB tool output budget; narrow the pattern/include"
                     % (MAX_OUTPUT_BYTES // 1024))

    skipped = []
    for key, label in (("binary", "binary"), ("undecodable", "non-UTF-8"),
                       ("oversized", "oversized(>%dMB)" % (MAX_FILE_BYTES // (1024 * 1024))),
                       ("skipped_sensitive", "sensitive"), ("unreadable", "unreadable")):
        if stats.get(key):
            skipped.append("%d %s" % (stats[key], label))
    if skipped:
        notes.append("skipped %s file(s)" % ", ".join(skipped))

    text = body
    if notes:
        text = (body + "\n\n" if body else "") + "\n".join(notes)
    if text:
        sys.stdout.write(text + "\n")
    return 0


def main():
    raw = sys.stdin.read() or "{}"
    try:
        args = json.loads(raw)
    except json.JSONDecodeError as exc:
        return fail("invalid args json: %s" % exc)

    if not isinstance(args, dict):
        return fail("args must be a JSON object")

    try:
        return run(args)
    except ValueError as exc:
        return fail(str(exc))
    except OSError as exc:
        return fail("grep failed: %s" % exc)


def selftest():
    import tempfile

    # 超长行截断
    assert clip_line("abc") == ("abc", False)
    clipped_text, was_clipped = clip_line("a" * (MAX_LINE_CHARS + 3))
    assert was_clipped and clipped_text.startswith("a" * MAX_LINE_CHARS)
    assert "3 more chars" in clipped_text

    # 花括号展开（含多层）
    assert expand_braces("*.py") == ["*.py"]
    assert expand_braces("*.{ts,tsx}") == ["*.ts", "*.tsx"]
    assert sorted(expand_braces("*.{a,{b,c}}")) == ["*.a", "*.b", "*.c"]
    assert expand_braces("") == [""]

    # include 匹配：basename / 相对路径
    m = compile_include("*.{py,md}")
    assert m("a/b/x.py", "x.py") and m("x.md", "x.md") and not m("x.txt", "x.txt")
    m2 = compile_include("src/*.py")
    assert m2("src/x.py", "x.py") and not m2("other/x.py", "x.py")
    assert compile_include("")("any/thing.txt", "thing.txt")

    # 参数
    assert parse_int({}, "max_results", DEFAULT_MAX_RESULTS, 1) == DEFAULT_MAX_RESULTS
    assert parse_int({"max_results": "10"}, "max_results", 0, 1) == 10
    for bad in (-1, 0, "x", True):
        try:
            parse_int({"max_results": bad}, "max_results", DEFAULT_MAX_RESULTS, 1)
        except ValueError:
            continue
        raise AssertionError("max_results 应当报错: %r" % (bad,))

    assert parse_bool({}, "case_insensitive") is False
    assert parse_bool({"case_insensitive": "true"}, "case_insensitive") is True
    assert parse_bool({"case_insensitive": 0}, "case_insensitive") is False
    try:
        parse_bool({"case_insensitive": "maybe"}, "case_insensitive")
    except ValueError:
        pass
    else:
        raise AssertionError("非法布尔应当报错")

    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        (root / "a.py").write_text("Hello World\nno hit\nhello again\n", encoding="utf-8")
        (root / "b.md").write_text("hello doc\n", encoding="utf-8")
        (root / "sub").mkdir()
        (root / "sub" / "c.py").write_text("hello nested\n", encoding="utf-8")
        (root / "bin.dat").write_bytes(b"\x00hello")
        (root / ".env").write_text("hello secret\n", encoding="utf-8")

        regex = re.compile("hello", re.IGNORECASE)

        # 目录递归 + 大小写不敏感 + 命中数与文件数
        lines, stats = _collect(root, regex, compile_include(""), DEFAULT_MAX_RESULTS)
        assert stats["matches"] == 4, stats           # a.py x2 / b.md / sub/c.py
        assert stats["binary"] == 1 and stats.get("skipped_sensitive") == 1, stats
        assert any("a.py:1:" in item for item in lines), lines
        assert any("sub/c.py:1:" in item for item in lines), lines

        # include 过滤
        lines, stats = _collect(root, regex, compile_include("*.py"), DEFAULT_MAX_RESULTS)
        assert stats["matches"] == 3, stats           # a.py x2 + sub/c.py
        assert not any("b.md" in item for item in lines)

        # max_results 截断
        lines, stats = _collect(root, regex, compile_include(""), 2)
        assert stats["matches"] == 2, stats

        # 单文件扫描
        single = root / "a.py"
        assert resolve_search_root(str(single)) == single.resolve()

        # 绝对路径可用（与 read 工具同口径，不受工作区限制）
        assert resolve_search_root(tmp) == root.resolve()

    print("selftest ok")


def _collect(root, regex, matcher, max_hits):
    """selftest 辅助：按与 run() 相同的口径扫描，返回 (行清单, 统计)。"""
    lines = []
    stats = {"matches": 0, "binary": 0, "undecodable": 0, "oversized": 0, "unreadable": 0, "files": 0}

    def sink(display_path, lineno, text):
        lines.append("%s:%d:%s" % (display_path, lineno, text))

    for path in iter_files(root, matcher):
        if stats["matches"] >= max_hits:
            break
        if _is_sensitive(path):
            stats["skipped_sensitive"] = stats.get("skipped_sensitive", 0) + 1
            continue
        stats["files"] += 1
        try:
            display = path.relative_to(root).as_posix()
        except ValueError:
            display = str(path)
        search_file(path, regex, display, max_hits, sink, stats)
    return lines, stats


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        sys.exit(main())

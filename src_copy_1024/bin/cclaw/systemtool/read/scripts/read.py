#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
read.py —— 系统工具 Read 的脚本实现（bin/cclaw/systemtool/read）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"file_path": "...", "offset": 0, "limit": 2000}
  - 结果（文本）写 stdout，退出码 0 = 成功；失败退出码非 0（stderr 为可读错误，stdout 为 {"error":...}）

与 read.json 声明的语义一一对应：
  - 只能读文件，不能读目录（目录报错，并提示改用 Glob/Grep）
  - file_path 支持绝对路径、相对路径、~/ 路径
  - 默认从文件开头读 2000 行；offset 为 **0 基行号**，limit 为最多读取的行数
  - 单行超过 2000 字符截断并就地标注
  - 单文件上限 1MB；仅 UTF-8 文本（含 NUL 字节视为二进制，直接报错）
  - 只接受**普通文件**：管道/设备/socket 等特殊文件直接报错（避免读 /dev/zero 之类把调用挂死）
  - 自动剥离 UTF-8 BOM，避免首行凭空多出一个不可见字符
  - 错误提示与输出注记统一为中文，便于模型直接理解并自我纠正

输出：每行形如 "  1234\t原文"，行号与文件真实行号一致（0 基 offset 也是按这个口径），
便于后续 Edit 工具按行定位；被截断/未读完的部分在末尾以 "…" 提示续读方式。

**不再限制目标必须位于工作区内**；敏感文件（密钥/凭据/库文件）仍被拒绝。

输出预算：execbox 的 stdout 上限为 256KB，本脚本自带 200KB 预算，超预算主动收尾并提示续读
（若放任 stdout 超限，整次调用会被判失败，模型连已读到的内容都拿不到）。

自检：python3 scripts/read.py --selftest
"""

import json
import os
import stat
import sys
from pathlib import Path

TOOL_NAME = "read"
VERSION = "1.0.0"

DEFAULT_LIMIT = 2000              # 默认读取行数（read.json）
MAX_LINE_CHARS = 2000             # 单行字符上限（read.json：超过则截断）
MAX_FILE_BYTES = 1024 * 1024      # 单文件上限 1MB（read.json）
MAX_OUTPUT_BYTES = 200 * 1024     # 自身输出预算，需小于 execbox 的 256KB

_SENSITIVE_NAMES = {".env", ".netrc", ".pgpass", "id_rsa", "id_ecdsa", "credentials", "known_hosts"}
_SENSITIVE_SUFFIX = (".pem", ".key", ".p12", ".pfx", ".db", ".sqlite", ".sqlite3", ".crt")
_SENSITIVE_HINT = ("password", "passwd", "secret", "credential", "signkey", "token", "privatekey")

# --------------------------------------------------------------------------
# 路径解析
# --------------------------------------------------------------------------

# 路径解析基准：agent 工作区（由服务端注入 CCLAW_AGENT_WORKSPACE；缺省回退 <bin>/workspace）
def _agent_workspace_root():
    env = (os.environ.get("CCLAW_AGENT_WORKSPACE") or "").strip()
    return Path(env) if env else _workspace_root()

def _is_sensitive(real_path: Path) -> bool:
    name = real_path.name.lower()
    if name in _SENSITIVE_NAMES or name.endswith(_SENSITIVE_SUFFIX):
        return True

    low = str(real_path).lower()
    return any(h in low for h in _SENSITIVE_HINT)

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


def resolve_target(raw):
    """把 file_path 解析为存在的真实路径；找不到时抛 ValueError。

    支持三种写法：绝对路径、~/ 路径、工作区相对路径。
    **不再限制目标必须位于 agent 工作区内**；仍保留两道约束：
      - 敏感文件（密钥/凭据/库文件，见 _is_sensitive）拒绝读取；
      - 统一 resolve 成真实路径，避免同一文件出现多种写法。
    """
    text = str(raw or "").strip()
    if text == "":
        raise ValueError("必须提供 file_path（要读取的文件路径）")

    # ~/ 与 ~user/ 展开为家目录；无法展开（用户不存在等）按非法路径报错
    if text.startswith("~"):
        expanded = os.path.expanduser(text)
        if expanded == text:
            raise ValueError("无法展开家目录路径：%s" % text)
        text = expanded

    hint = ""
    if os.path.isabs(text):
        candidate = Path(text)
    else:
        # 相对路径落到 agent 工作区（进程 cwd 也是它，见 script_runner.go 的 cmd.Dir）
        root = _agent_workspace_root()
        candidate = root / text
        hint = "（工作区：%s）" % root.resolve()

    try:
        real_target = candidate.resolve(strict=True)
    except FileNotFoundError:
        raise ValueError("文件不存在：%s%s" % (text, hint))
    except OSError as exc:
        raise ValueError("无法解析路径 %s：%s" % (text, exc))

    if _is_sensitive(real_target):
        raise ValueError("拒绝读取敏感文件：%s（密钥/凭据/数据库类文件不允许读取）" % real_target.name)

    return real_target


# --------------------------------------------------------------------------
# 读取与切行
# --------------------------------------------------------------------------
def split_lines(data):
    """按 LF/CRLF 切行；末尾换行不产生额外空行（CRLF 的 \\r 在此剥掉）。"""
    lines = data.split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    return [line[:-1] if line.endswith("\r") else line for line in lines]


def load_file_lines(path):
    """读文件并切行；目录/特殊文件/超限/二进制/非 UTF-8 一律抛 ValueError（提示要能直接给模型看）。"""
    info = path.stat()
    if stat.S_ISDIR(info.st_mode):
        raise ValueError("该路径是目录而不是文件：%s（请改用 Glob/Grep 工具）" % path)
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("不是普通文件，无法读取：%s（管道/设备/socket 等特殊文件不支持）" % path)

    if info.st_size > MAX_FILE_BYTES:
        raise ValueError("文件过大：%d 字节，超过 %d 字节上限：%s" % (info.st_size, MAX_FILE_BYTES, path))

    raw = path.read_bytes()
    if b"\x00" in raw:
        raise ValueError("二进制文件（检测到 NUL 字节），只能读取 UTF-8 文本文件：%s" % path)
    try:
        data = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError("不是 UTF-8 文本文件：%s（%s）" % (path, exc))

    # 剥离 UTF-8 BOM，否则首行会带上不可见的 \ufeff，导致 Edit 匹配失败
    if data.startswith("\ufeff"):
        data = data[1:]

    return split_lines(data)


def clip_line(text, max_chars=MAX_LINE_CHARS):
    """超长行截断并就地标注，返回 (文本, 是否截断)。"""
    if len(text) <= max_chars:
        return text, False
    return "%s …[本行已截断，还有 %d 字符]" % (text[:max_chars], len(text) - max_chars), True


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


def render(lines, offset, limit):
    """按 offset(0 基)/limit 渲染带行号的窗口，返回 (正文, 提示列表)。"""
    total = len(lines)
    if total == 0:
        return "", ["（文件为空：共 0 行）"]
    if offset >= total:
        return "", ["（offset=%d 超出文件末尾：文件共 %d 行）" % (offset, total)]

    start = min(offset, total)
    window = lines[start:start + limit]

    chunks = []
    used = 0
    clipped = 0
    stopped = False
    for idx, text in enumerate(window):
        text, was_clipped = clip_line(text)
        if was_clipped:
            clipped += 1
        rendered = "%6d\t%s" % (start + idx + 1, text)
        cost = len(rendered.encode("utf-8")) + 1
        if chunks and used + cost > MAX_OUTPUT_BYTES:
            stopped = True
            break
        chunks.append(rendered)
        used += cost

    end = start + len(chunks)
    notes = []
    if stopped:
        notes.append("… 输出在第 %d 行处因工具输出预算而截断；请用 offset=%d 继续读取" % (end, end))
    if end < total:
        notes.append("… 还有 %d 行未显示（文件共 %d 行）；请用 offset=%d 继续读取"
                     % (total - end, total, end))
    if clipped:
        notes.append("… 有 %d 行超过 %d 字符，已被截断" % (clipped, MAX_LINE_CHARS))

    return "\n".join(chunks), notes


# --------------------------------------------------------------------------
# 入口
# --------------------------------------------------------------------------
def fail(message, code=1):
    """失败：stdout 给 JSON（便于直接手跑脚本核对），stderr 给可读错误（execbox 取它做错误详情）。"""
    sys.stdout.write(json.dumps({"error": message}, ensure_ascii=False) + "\n")
    sys.stderr.write(message + "\n")
    return code


def main():
    raw = sys.stdin.read() or "{}"
    try:
        args = json.loads(raw)
    except json.JSONDecodeError as exc:
        return fail("参数 JSON 解析失败：%s" % exc)

    if not isinstance(args, dict):
        return fail("参数必须是 JSON 对象")

    try:
        offset = parse_int(args, "offset", 0, 0)
        limit = parse_int(args, "limit", DEFAULT_LIMIT, 1)
        target = resolve_target(args.get("file_path"))
        lines = load_file_lines(target)
        body, notes = render(lines, offset, limit)
    except ValueError as exc:
        return fail(str(exc))
    except OSError as exc:
        return fail("读取失败：%s" % exc)

    text = body
    if notes:
        text = (body + "\n\n" if body else "") + "\n".join(notes)
    if text:
        sys.stdout.write(text + "\n")
    return 0


def selftest():
    import tempfile

    # 超长行截断
    assert clip_line("abc") == ("abc", False)
    clipped_text, was_clipped = clip_line("a" * (MAX_LINE_CHARS + 3))
    assert was_clipped and clipped_text.startswith("a" * MAX_LINE_CHARS)
    assert "3 字符" in clipped_text

    # 切行：LF/CRLF 一致，末尾换行不产生空行，孤立 CR 不拆
    assert split_lines("a\nb\n") == ["a", "b"]
    assert split_lines("a\r\nb") == ["a", "b"]
    assert split_lines("a\r\nb\rc") == ["a", "b\rc"]
    assert split_lines("") == []

    # 参数：容忍字符串/浮点整数，拒绝负数、0 limit、布尔、非数字
    assert parse_int({}, "offset", 0, 0) == 0
    assert parse_int({"offset": 3}, "offset", 0, 0) == 3
    assert parse_int({"offset": "3"}, "offset", 0, 0) == 3
    assert parse_int({"offset": 3.0}, "offset", 0, 0) == 3
    assert parse_int({"limit": ""}, "limit", DEFAULT_LIMIT, 1) == DEFAULT_LIMIT
    assert parse_int({"limit": "10"}, "limit", DEFAULT_LIMIT, 1) == 10
    assert parse_int({"limit": None}, "limit", DEFAULT_LIMIT, 1) == DEFAULT_LIMIT  # null 视为未传
    for bad_key, bad_value in [("offset", -1), ("offset", True), ("offset", "x"),
                               ("offset", 1.5), ("limit", 0), ("limit", -5), ("limit", "abc")]:
        try:
            parse_int({bad_key: bad_value}, bad_key, 0 if bad_key == "offset" else DEFAULT_LIMIT,
                      0 if bad_key == "offset" else 1)
        except ValueError:
            continue
        raise AssertionError("parse_int 应当报错: %s=%r" % (bad_key, bad_value))

    with tempfile.TemporaryDirectory() as tmp:
        sample = Path(tmp) / "sample.txt"
        sample.write_text("\n".join("line%d" % i for i in range(1, 11)) + "\n", encoding="utf-8")

        lines = load_file_lines(sample)
        assert len(lines) == 10, lines

        # 默认窗口：带真实行号 + 续读提示
        body, notes = render(lines, 0, 3)
        assert body.splitlines() == ["     1\tline1", "     2\tline2", "     3\tline3"], body
        assert notes and "offset=3" in notes[0]

        # 读尾部：无提示
        body, notes = render(lines, 8, 5)
        assert "line10" in body and not notes, (body, notes)

        # offset 越界 / 空文件
        body, notes = render(lines, 99, 5)
        assert body == "" and "超出文件末尾" in notes[0], (body, notes)
        body, notes = render([], 0, 5)
        assert body == "" and "为空" in notes[0], (body, notes)

        # 目录 / 二进制 / 超大文件：都必须报错而不是静默返回
        try:
            load_file_lines(Path(tmp))
        except ValueError:
            pass
        else:
            raise AssertionError("目录应当报错")

        binary = Path(tmp) / "b.bin"
        binary.write_bytes(b"\x00\x01\x02")
        try:
            load_file_lines(binary)
        except ValueError:
            pass
        else:
            raise AssertionError("二进制文件应当报错")

        big = Path(tmp) / "big.txt"
        big.write_bytes(b"a" * (MAX_FILE_BYTES + 1))
        try:
            load_file_lines(big)
        except ValueError:
            pass
        else:
            raise AssertionError("超大文件应当报错")

        # 特殊文件（FIFO）必须报错：放宽工作区限制后必须防住读管道/设备挂死
        if hasattr(os, "mkfifo"):
            fifo = Path(tmp) / "pipe.fifo"
            os.mkfifo(fifo)
            try:
                load_file_lines(fifo)
            except ValueError:
                pass
            else:
                raise AssertionError("FIFO 等特殊文件应当报错")

        # UTF-8 BOM 应被剥离，首行不带不可见字符
        bom_file = Path(tmp) / "bom.txt"
        bom_file.write_bytes("\ufeffhello\n".encode("utf-8"))
        assert load_file_lines(bom_file) == ["hello"], load_file_lines(bom_file)

        # 相对路径落点：工作区内相对路径可解（<=> <bin>/workspace 回退可用）
        assert _workspace_root().name == "workspace"

        # 放宽后：工作区之外的绝对路径同样可读（本次去掉“仅限工作目录”的限制）
        assert resolve_target(str(sample)) == sample.resolve()

    print("selftest ok")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        sys.exit(main())

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
edit.py —— 系统工具 Edit 的脚本实现（bin/cclaw/systemtool/edit）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"file_path": "...", "old_string": "...", "new_string": "...", "replace_all": false}
  - 结果（文本）写 stdout，退出码 0 = 成功；失败退出码非 0（stderr 为可读错误，stdout 为 {"error":...}）

与 edit.json 声明的语义一一对应：
  - **按字面匹配**做精确替换（不是正则，不解释任何元字符）
  - old_string 必须唯一；不唯一时报错并列出现有行号，提示补充上下文或 replace_all=true
  - old_string 与 new_string 不能相同；old_string 不能为空（新建/整体覆盖请用 Write）
  - file_path 支持绝对路径、~/ 路径、工作区相对路径；必须是已存在的普通文件

与 Read 的互操作（重要）：
  - Read 输出时会把 CRLF 的 \\r 剥掉、并剥离 UTF-8 BOM，因此模型看到的文本是 LF + 无 BOM。
  - 本工具先按「LF 归一化 + 去 BOM」的文本匹配；命中后再**还原**成文件原本的换行风格与 BOM。
  - 若归一化后匹配不到，会退回原始文本再试一次（兼容模型直接给出 CRLF 的情况）。
  - 这样无论文件是 LF 还是 CRLF，Read → Edit 的往返都不会因换行符差异而失败。

安全与原子性：
  - 拒绝敏感文件名（.env / *.pem / *.key / *.db 等）；拒绝二进制/非 UTF-8/超大文件
  - 先写同目录临时文件再 os.replace 原子替换，并保留原文件权限位

自检：python3 scripts/edit.py --selftest
"""

import json
import os
import sys
import tempfile
from pathlib import Path

TOOL_NAME = "edit"
VERSION = "1.0.0"

MAX_FILE_BYTES = 2 * 1024 * 1024   # 单文件上限 2MB（编辑大文件应改用脚本处理）
MAX_NEW_BYTES = 48 * 1024          # 新增文本上限（受 execbox 的 64KB 参数 JSON 上限约束）

_SENSITIVE_NAMES = {".env", ".netrc", ".pgpass", "id_rsa", "id_ecdsa", "credentials", "known_hosts"}
_SENSITIVE_SUFFIX = (".pem", ".key", ".p12", ".pfx", ".db", ".sqlite", ".sqlite3", ".crt")


# --------------------------------------------------------------------------
# 路径解析
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
    return name in _SENSITIVE_NAMES or name.endswith(_SENSITIVE_SUFFIX)


def resolve_target(raw):
    """把 file_path 解析为存在的真实路径（必须已存在）。"""
    text = str(raw or "").strip()
    if text == "":
        raise ValueError("必须提供 file_path（要编辑的文件路径）")

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
        real = candidate.resolve(strict=True)
    except FileNotFoundError:
        raise ValueError("文件不存在：%s%s；新建文件请用 Write 工具" % (text, hint))
    except OSError as exc:
        raise ValueError("无法解析路径 %s：%s" % (text, exc))

    if _is_sensitive(real):
        raise ValueError("拒绝编辑敏感文件：%s（密钥/凭据/数据库类文件不允许编辑）" % real.name)
    return real


# --------------------------------------------------------------------------
# 读取与换行风格处理
# --------------------------------------------------------------------------

def load_text(path):
    """读取文本，返回 (原文, 是否带 BOM, 是否纯 CRLF)。非 UTF-8/二进制/超大一律报错。"""
    if path.is_dir():
        raise ValueError("该路径是目录而不是文件：%s" % path)
    if not path.is_file():
        raise ValueError("不是普通文件，无法编辑：%s（管道/设备/socket 等特殊文件不支持）" % path)

    size = path.stat().st_size
    if size > MAX_FILE_BYTES:
        raise ValueError("文件过大：%d 字节，超过 %d 字节上限：%s" % (size, MAX_FILE_BYTES, path))

    raw = path.read_bytes()
    if b"\x00" in raw:
        raise ValueError("二进制文件（检测到 NUL 字节），只能编辑 UTF-8 文本文件：%s" % path)
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError("不是 UTF-8 文本文件：%s（%s）" % (path, exc))

    bom = text.startswith("\ufeff")
    if bom:
        text = text[1:]

    lf_total = text.count("\n")
    crlf_total = text.count("\r\n")
    pure_crlf = lf_total > 0 and crlf_total == lf_total
    return text, bom, pure_crlf


def _line_numbers(text, needle, limit=5):
    """返回 needle 在 text 中各次出现的行号（最多 limit 个）。"""
    out = []
    start = 0
    while len(out) < limit:
        idx = text.find(needle, start)
        if idx < 0:
            break
        out.append(text.count("\n", 0, idx) + 1)
        start = idx + len(needle)
    return out


def atomic_write(target, payload):
    """先写同目录临时文件，再原子替换；覆盖时保留原文件权限位。"""
    existing_mode = None
    try:
        existing_mode = target.stat().st_mode & 0o777
    except OSError:
        existing_mode = None

    fd, tmp_name = tempfile.mkstemp(prefix=".cclaw-edit-", dir=str(target.parent))
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        if existing_mode is not None:
            try:
                os.chmod(tmp_name, existing_mode)
            except OSError:
                pass
        os.replace(tmp_name, str(target))
        tmp_name = None
    finally:
        if tmp_name is not None:
            try:
                os.unlink(tmp_name)
            except OSError:
                pass


# --------------------------------------------------------------------------
# 入口
# --------------------------------------------------------------------------

def fail(message, code=1):
    """失败：stdout 给 JSON（便于直接手跑脚本核对），stderr 给可读错误（execbox 取它做错误详情）。"""
    sys.stdout.write(json.dumps({"error": message}, ensure_ascii=False) + "\n")
    sys.stderr.write(message + "\n")
    return code


def run(args):
    if "file_path" not in args or args.get("file_path") is None:
        raise ValueError("必须提供 file_path（要编辑的文件路径）")
    if "old_string" not in args or args.get("old_string") is None:
        raise ValueError("必须提供 old_string（要被替换的原文；新建或整体覆盖请用 Write 工具）")
    if "new_string" not in args or args.get("new_string") is None:
        raise ValueError("必须提供 new_string（替换后的文本；删除内容请传空字符串）")

    old = args.get("old_string")
    new = args.get("new_string")
    if not isinstance(old, str) or not isinstance(new, str):
        raise ValueError("old_string 与 new_string 都必须是字符串")

    if old == "":
        raise ValueError("old_string 不能为空。新建文件或整体覆盖请用 Write 工具；追加内容请给出定位用的原文片段。")
    if old == new:
        raise ValueError("old_string 与 new_string 相同，没有任何改动。")

    replace_all = parse_bool(args, "replace_all", False)

    if len(new.encode("utf-8")) > MAX_NEW_BYTES:
        raise ValueError("new_string 过大：超过 %d 字节上限（受工具参数 64KB 传输上限约束），请拆成多次编辑"
                         % MAX_NEW_BYTES)

    target = resolve_target(args.get("file_path"))
    raw_text, bom, pure_crlf = load_text(target)

    # 优先在「LF 归一化」的文本上匹配（与 Read 的输出口径一致）；未命中再退回原文
    candidates = []
    if pure_crlf:
        candidates.append(raw_text.replace("\r\n", "\n"))
    candidates.append(raw_text)

    source = None
    for candidate in candidates:
        if candidate.count(old) > 0:
            source = candidate
            break

    if source is None:
        raise ValueError(
            "未找到 old_string（按字面精确匹配，不是正则）。可能原因：\n"
            "1) 缩进/空格或引号与文件实际内容不一致；\n"
            "2) 文件已被其他操作修改。\n"
            "请先用 Read 读取该文件的对应区域，再逐字符复制原文后重试。")

    total = source.count(old)
    if total > 1 and not replace_all:
        lines = _line_numbers(source, old)
        raise ValueError(
            "old_string 在文件中出现了 %d 次，不唯一，无法确定替换哪一处（行号：%s…）。\n"
            "请补充上下文使 old_string 唯一，或设置 replace_all=true 全部替换。"
            % (total, "、".join(str(n) for n in lines)))

    result_text = source.replace(old, new) if replace_all else source.replace(old, new, 1)
    replaced = total if replace_all else 1

    # 还原文件原本的换行风格与 BOM
    if bom:
        result_text = "\ufeff" + result_text
    if pure_crlf and "\r\n" not in source:
        result_text = result_text.replace("\r\n", "\n").replace("\n", "\r\n")

    atomic_write(target, result_text.encode("utf-8"))

    try:
        shown = target.relative_to(_agent_workspace_root().resolve()).as_posix()
    except (ValueError, OSError):
        shown = str(target)

    text = "已修改 %s：替换 %d 处" % (shown, replaced)
    if lines := _line_numbers(source, old):
        text += "（起始行号：%s）" % "、".join(str(n) for n in lines)
    if replaced > 1:
        text += "；若这不是预期结果，请用 Read 复查后再调整"
    sys.stdout.write(text + "\n")
    return 0


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
        return fail("编辑失败：%s" % exc)


def selftest():
    import tempfile as _tempfile

    with _tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        sample = root / "a.txt"
        sample.write_text("alpha\nbeta\ngamma\n", encoding="utf-8")

        # 基本替换 + 行号提示
        assert run({"file_path": str(sample), "old_string": "beta", "new_string": "BETA"}) == 0
        assert sample.read_text(encoding="utf-8") == "alpha\nBETA\ngamma\n"

        # 未命中：报错
        try:
            run({"file_path": str(sample), "old_string": "nope", "new_string": "x"})
        except ValueError as exc:
            assert "未找到" in str(exc), exc
        else:
            raise AssertionError("未命中应当报错")

        # 不唯一：报错
        sample.write_text("dup\ndup\n", encoding="utf-8")
        try:
            run({"file_path": str(sample), "old_string": "dup", "new_string": "x"})
        except ValueError as exc:
            assert "不唯一" in str(exc), exc
        else:
            raise AssertionError("不唯一应当报错")

        # replace_all
        assert run({"file_path": str(sample), "old_string": "dup", "new_string": "x", "replace_all": True}) == 0
        assert sample.read_text(encoding="utf-8") == "x\nx\n"

        # old==new / old 为空
        for bad in ({"old_string": "x", "new_string": "x"}, {"old_string": "", "new_string": "y"}):
            try:
                run(dict(file_path=str(sample), **bad))
            except ValueError:
                continue
            raise AssertionError("非法参数应当报错: %r" % (bad,))

        # CRLF 互操作：Read 看到的是 LF，Edit 按 LF 匹配后应保持 CRLF
        crlf = root / "crlf.txt"
        crlf.write_bytes("one\r\ntwo\r\n".encode("utf-8"))
        run({"file_path": str(crlf), "old_string": "one\ntwo", "new_string": "one\nTWO"})
        assert crlf.read_bytes() == "one\r\nTWO\r\n".encode("utf-8"), crlf.read_bytes()

        # BOM 保留
        bom_file = root / "bom.txt"
        bom_file.write_bytes("\ufeffhello\n".encode("utf-8"))
        run({"file_path": str(bom_file), "old_string": "hello", "new_string": "hi"})
        assert bom_file.read_bytes().startswith("\ufeff".encode("utf-8")), bom_file.read_bytes()
        assert load_text(bom_file) == ("hi\n", True, False), load_text(bom_file)

        # 删除内容（new_string 为空）
        sample.write_text("keep\nremove\n", encoding="utf-8")
        run({"file_path": str(sample), "old_string": "remove\n", "new_string": ""})
        assert sample.read_text(encoding="utf-8") == "keep\n"

        # 敏感文件拒绝
        secret = root / ".env"
        secret.write_text("x", encoding="utf-8")
        try:
            run({"file_path": str(secret), "old_string": "x", "new_string": "y"})
        except ValueError as exc:
            assert "敏感" in str(exc), exc
        else:
            raise AssertionError("敏感文件应当报错")

    print("selftest ok")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        sys.exit(main())

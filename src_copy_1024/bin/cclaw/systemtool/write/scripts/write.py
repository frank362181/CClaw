#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
write.py —— 系统工具 Write 的脚本实现（bin/cclaw/systemtool/write）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"file_path": "...", "content": "..."}
  - 结果（文本）写 stdout，退出码 0 = 成功；失败退出码非 0（stderr 为可读错误，stdout 为 {"error":...}）

与 write.json 声明的语义一一对应：
  - 新建文件，或**整体覆盖**已有文件（只改一部分请用 Edit，不要用本工具）
  - file_path 支持绝对路径、~/ 路径、工作区相对路径；父目录不存在时自动创建
  - 内容按 UTF-8 原样写入（不做换行符转换；content 里带什么就写什么）
  - 单次写入内容上限 48KB：execbox 的参数 JSON 上限是 64KB，超过即无法送达，
    此处提前拦住并给出可执行的建议（改用 Edit 分段写入）

安全与原子性：
  - 拒绝敏感文件名（.env / *.pem / *.key / *.db 等密钥、凭据、数据库文件）
  - 目标必须是普通文件：目录、管道、设备、socket 一律拒绝（避免写 /dev/sda 之类的事故）
  - 先写同目录临时文件再 os.replace 原子替换：中途失败不会把原文件截断成空文件
  - 覆盖已有文件时保留其权限位（不因临时文件而变成 0600）

自检：python3 scripts/write.py --selftest
"""

import json
import os
import sys
import tempfile
from pathlib import Path

TOOL_NAME = "write"
VERSION = "1.0.0"

MAX_CONTENT_BYTES = 48 * 1024     # 单次写入内容上限（受 execbox 的 64KB 参数 JSON 上限约束）

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
    """按文件名/后缀判定敏感文件（不做全路径关键词匹配，避免误杀整个子目录）。"""
    name = real_path.name.lower()
    return name in _SENSITIVE_NAMES or name.endswith(_SENSITIVE_SUFFIX)


def _resolve_for_write(raw):
    """把「可能还不存在」的目标路径解析成真实路径。

    逐级向上找到第一个已存在的祖先并 resolve（处理 macOS 的 /var → /private/var
    以及路径中的符号链接），再把缺失的各级名字拼回去。
    """
    text = str(raw or "").strip()
    if text == "":
        raise ValueError("必须提供 file_path（要写入的文件路径）")

    if text.startswith("~"):
        expanded = os.path.expanduser(text)
        if expanded == text:
            raise ValueError("无法展开家目录路径：%s" % text)
        text = expanded

    if os.path.isabs(text):
        candidate = Path(text)
    else:
        candidate = _agent_workspace_root() / text

    candidate = Path(os.path.abspath(str(candidate)))

    missing = []
    current = candidate
    while True:
        try:
            resolved = current.resolve(strict=True)
        except FileNotFoundError:
            missing.append(current.name)
            parent = current.parent
            if parent == current:
                raise ValueError("无法解析目标路径：%s" % candidate)
            current = parent
            continue
        except OSError as exc:
            raise ValueError("无法解析目标路径 %s：%s" % (candidate, exc))
        break

    for name in reversed(missing):
        resolved = resolved / name
    return resolved


# --------------------------------------------------------------------------
# 写入
# --------------------------------------------------------------------------

def _default_file_mode():
    """新文件的权限：0666 & ~umask。mkstemp 默认 0600，对产物文件（如 index.html）过严。"""
    try:
        current = os.umask(0)
        os.umask(current)
    except OSError:
        return 0o644
    return 0o666 & ~current


def atomic_write(target, payload):
    """先写同目录临时文件，再原子替换；覆盖时保留原文件权限位，新建时按 umask 计算。"""
    parent = target.parent
    try:
        os.makedirs(parent, exist_ok=True)
    except OSError as exc:
        raise ValueError("无法创建父目录 %s：%s" % (parent, exc))

    existing_mode = None
    if target.exists():
        try:
            existing_mode = target.stat().st_mode & 0o777
        except OSError:
            existing_mode = None

    mode = existing_mode if existing_mode is not None else _default_file_mode()

    fd, tmp_name = tempfile.mkstemp(prefix=".cclaw-write-", dir=str(parent))
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        try:
            os.chmod(tmp_name, mode)
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


def run(args):
    if "file_path" not in args or args.get("file_path") is None:
        raise ValueError("必须提供 file_path（要写入的文件路径）")
    if "content" not in args or args.get("content") is None:
        raise ValueError("必须提供 content（要写入的完整内容；写空文件请显式传空字符串）")

    content = args.get("content")
    if not isinstance(content, str):
        raise ValueError("content 必须是字符串，实际收到 %r" % type(content).__name__)

    payload = content.encode("utf-8")
    if len(payload) > MAX_CONTENT_BYTES:
        raise ValueError(
            "写入内容过大：%d 字节，单次上限 %d 字节（受工具参数 64KB 传输上限约束）。"
            "请拆成多次写入：先用本工具写入主体，再用 Edit 追加/替换后续内容。"
            % (len(payload), MAX_CONTENT_BYTES))

    target = _resolve_for_write(args.get("file_path"))

    if _is_sensitive(target):
        raise ValueError("拒绝写入敏感文件：%s（密钥/凭据/数据库类文件不允许写入）" % target.name)

    existed = target.exists()
    if existed:
        if target.is_dir():
            raise ValueError("目标是一个目录，无法写入：%s" % target)
        if not target.is_file():
            raise ValueError("目标不是普通文件，无法写入：%s（管道/设备/socket 等特殊文件不支持）" % target)

    atomic_write(target, payload)

    try:
        display = target.relative_to(_agent_workspace_root().resolve())
        shown = display.as_posix()
    except (ValueError, OSError):
        shown = str(target)

    line_count = content.count("\n") + (1 if content and not content.endswith("\n") else 0)
    action = "覆盖" if existed else "新建"
    text = "%s %s（%d 字节，%d 行）" % (action, shown, len(payload), line_count)
    if existed:
        text += "\n\n注意：已有内容已被整体替换；如需局部修改，请改用 Edit 工具。"
    sys.stdout.write(text + "\n")
    return 0


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
        return run(args)
    except ValueError as exc:
        return fail(str(exc))
    except OSError as exc:
        return fail("写入失败：%s" % exc)


def selftest():
    import tempfile as _tempfile

    with _tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)

        # 新建 + 父目录自动创建
        target = root / "a" / "b" / "new.txt"
        resolved = _resolve_for_write(str(target))
        assert resolved == target.resolve(), resolved
        atomic_write(resolved, "hello\n".encode("utf-8"))
        assert target.read_text(encoding="utf-8") == "hello\n"

        # 新建文件的权限位应遵循 umask（而非 mkstemp 的 0600）
        created = root / "perm.txt"
        atomic_write(created, b"x")
        probe = os.umask(0)
        os.umask(probe)
        assert (created.stat().st_mode & 0o777) == (0o666 & ~probe), (
            oct(created.stat().st_mode & 0o777), oct(probe))

        # 覆盖：内容整体替换
        atomic_write(resolved, "bye\n".encode("utf-8"))
        assert target.read_text(encoding="utf-8") == "bye\n"

        # 覆盖时保留原权限位
        os.chmod(target, 0o640)
        atomic_write(resolved, "again\n".encode("utf-8"))
        assert (target.stat().st_mode & 0o777) == 0o640, oct(target.stat().st_mode & 0o777)

        # 目录不可写
        try:
            atomic_write(root, b"x")
        except (ValueError, IsADirectoryError, OSError):
            pass
        else:
            raise AssertionError("目录应当无法写入")

        # 敏感文件名拒绝
        assert _is_sensitive(Path("/tmp/.env"))
        assert _is_sensitive(Path("/tmp/server.key"))
        assert not _is_sensitive(Path("/tmp/report.md"))

        # 内容过大拒绝
        try:
            run({"file_path": str(root / "big.txt"), "content": "a" * (MAX_CONTENT_BYTES + 1)})
        except ValueError as exc:
            assert "过大" in str(exc), exc
        else:
            raise AssertionError("超大内容应当报错")

        # 缺参
        for bad in ({}, {"file_path": str(root / "x.txt")}, {"content": "x"}):
            try:
                run(bad)
            except ValueError:
                continue
            raise AssertionError("缺参应当报错: %r" % (bad,))

        # 敏感路径拒绝
        try:
            run({"file_path": str(root / ".env"), "content": "x"})
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

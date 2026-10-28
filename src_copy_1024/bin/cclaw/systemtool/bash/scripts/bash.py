#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
bash.py —— 系统工具 Bash 的脚本实现（bin/cclaw/systemtool/bash）

契约（与 cclawservice/harness/toolexecutor/script_runner.go 对齐）：
  - 参数 JSON 经 stdin 传入：{"command": "git status --short", "timeout": 30000}
  - 结果（文本）写 stdout；**命令自身失败也返回退出码 0**，把真实退出码写进结果正文
  - 仅当参数非法 / 命中危险命令拦截时返回非 0（此时没有需要保留的命令输出）

为什么命令失败也要 exit 0：
  执行器在脚本非 0 退出时**丢弃 stdout**，只把 stderr（或 stdout 的 {"error":...}）
  当作错误详情回传。若这里用非 0 退出，模型就看不到命令的真实输出与退出码，
  只能看到一句 "exit status 1"，无法自我纠错。因此命令结果一律走正文。

执行环境：
  - shell 优先 /bin/bash（缺失时回退 /bin/sh），cwd = agent 工作区
  - stdin 接 /dev/null：命令读取 stdin 会立即 EOF，不会把调用挂死
  - 独立进程组 + 超时/超量时 SIGKILL 整组（只杀 shell 会留下子进程）
  - 环境变量固化为非交互：PAGER/GIT_PAGER=cat、TERM=dumb、NO_COLOR=1、
    GIT_TERMINAL_PROMPT=0、DEBIAN_FRONTEND=noninteractive，避免分页器/彩色码/交互提问卡住
  - 超时默认 30s；上限 100s（执行器对脚本有 120s 硬超时，必须留出收尾余量）
  - 输出采集上限 4MB（超出即终止进程，防 `yes` 之类写满磁盘/内存），回传上限 200KB

危险命令拦截（防误操作，**不是安全边界**）：
  仅拦截少量不可逆操作（rm -rf /、mkfs、dd 写裸设备、关机重启、fork bomb、curl|sh、
  强推/强清 git 等）。真正的权限控制属于服务端 permission_handler 与沙箱；
  运维可用 CCLAW_BASH_ALLOW_DANGEROUS=1 临时关闭该拦截。

自检：python3 scripts/bash.py --selftest
"""

import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path

TOOL_NAME = "bash"
VERSION = "1.0.0"

DEFAULT_TIMEOUT_MS = 30_000       # 默认超时（bash.json）
MAX_TIMEOUT_MS = 100_000          # 单次上限：执行器 120s 硬超时，留 20s 收尾余量
MIN_TIMEOUT_MS = 1_000

MAX_CAPTURE_BYTES = 4 * 1024 * 1024   # 每个流的磁盘采集上限，超出即终止进程
LIMIT_STDOUT = 160 * 1024             # stdout 回传预算
LIMIT_STDERR = 40 * 1024              # stderr 回传预算（合计 200KB < 执行器 256KB）
POLL_INTERVAL = 0.05

# (正则, 原因)。只防误操作，不构成安全边界；模式针对小写扁平化后的命令。
_DANGEROUS_PATTERNS = [
    (r"\brm\s+-[a-z]*[rf][a-z]*\s+(?:--\s+)?(?:/|/\*|~|~/|\$home)(?:\s|;|&|\||$)", "递归删除根目录或家目录"),
    (r"\bmkfs(?:\.[a-z0-9]+)?\b", "格式化文件系统"),
    (r"\bdd\b[^|;&]*\bof=/dev/(?:sd|hd|disk|nvme|rdisk|mmcblk)", "向裸设备写入数据"),
    (r">\s*/dev/(?:sd|hd|disk|nvme|rdisk|mmcblk)", "向裸设备重定向写入"),
    (r"\b(?:shutdown|reboot|halt|poweroff)\b", "关机或重启"),
    (r":\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:", "fork bomb"),
    (r"\bchmod\s+-[a-z]*r[a-z]*\s+777\s+/\s*(?:$|;|&)", "递归修改根目录权限"),
    (r"\bgit\s+push\b[^|;&]*\s(?:--force|-f)\b", "强制推送远端"),
    (r"\bgit\s+clean\s+-[a-z]*[xdz]", "清除未跟踪文件"),
    (r"\b(?:curl|wget)\b[^|;&]*\|\s*(?:sudo\s+)?(?:ba|z|k|da)?sh\b", "下载后直接执行远程脚本"),
]


# --------------------------------------------------------------------------
# 路径与环境
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


def build_env(workspace):
    """构造子进程环境：禁掉分页器/彩色码/交互提问，避免命令卡在等输入。"""
    env = os.environ.copy()
    env.update({
        "CCLAW_AGENT_WORKSPACE": str(workspace),
        "PAGER": "cat",
        "GIT_PAGER": "cat",
        "GIT_TERMINAL_PROMPT": "0",
        "TERM": "dumb",
        "NO_COLOR": "1",
        "DEBIAN_FRONTEND": "noninteractive",
    })
    env.pop("LESS", None)
    return env


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


def check_dangerous(command):
    """拦截少量不可逆命令（防误操作；不是安全边界）。"""
    if (os.environ.get("CCLAW_BASH_ALLOW_DANGEROUS") or "").strip() == "1":
        return

    flat = " ".join(command.split()).lower()
    for pattern, reason in _DANGEROUS_PATTERNS:
        if re.search(pattern, flat):
            raise ValueError(
                "已拦截危险命令（%s）：%s\n"
                "该操作用作误操作防护。如确需执行，请由运维设置 "
                "CCLAW_BASH_ALLOW_DANGEROUS=1 后重试，或改用更精确的路径/范围。"
                % (reason, command))


# --------------------------------------------------------------------------
# 执行
# --------------------------------------------------------------------------

def _kill_process_group(proc):
    """SIGKILL 整个进程组（只杀 shell 会留下仍在写输出的子进程）。"""
    if proc.poll() is not None:
        return
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except (ProcessLookupError, PermissionError, OSError):
        try:
            proc.kill()
        except OSError:
            pass


def _read_head(handle, limit):
    """从临时文件头部读取至多 limit 字节，返回 (文本, 是否被截断)。"""
    try:
        handle.flush()
        handle.seek(0)
        data = handle.read(limit + 1)
    except OSError:
        return "", False
    truncated = len(data) > limit
    return data[:limit].decode("utf-8", "replace"), truncated


def execute(command, timeout_ms, workspace):
    """执行命令，返回渲染好的结果正文（不含退出码，失败也不抛异常）。"""
    shell = shutil.which("bash") or "/bin/bash"
    if not os.path.exists(shell):
        shell = "/bin/sh"

    started = time.monotonic()
    timed_out = False
    over_capture = False

    with tempfile.TemporaryFile() as out_f, tempfile.TemporaryFile() as err_f:
        proc = subprocess.Popen(
            command,
            shell=True,
            executable=shell,
            cwd=str(workspace),
            stdin=subprocess.DEVNULL,
            stdout=out_f,
            stderr=err_f,
            start_new_session=True,
            env=build_env(workspace),
        )

        while True:
            if proc.poll() is not None:
                break
            if (time.monotonic() - started) * 1000 > timeout_ms:
                timed_out = True
                _kill_process_group(proc)
                proc.wait()
                break
            try:
                if (os.fstat(out_f.fileno()).st_size > MAX_CAPTURE_BYTES
                        or os.fstat(err_f.fileno()).st_size > MAX_CAPTURE_BYTES):
                    over_capture = True
                    _kill_process_group(proc)
                    proc.wait()
                    break
            except OSError:
                pass
            time.sleep(POLL_INTERVAL)

        elapsed_ms = int((time.monotonic() - started) * 1000)
        code = proc.returncode
        stdout_text, out_trunc = _read_head(out_f, LIMIT_STDOUT)
        stderr_text, err_trunc = _read_head(err_f, LIMIT_STDERR)

    stdout_text = stdout_text.rstrip("\n")
    stderr_text = stderr_text.rstrip("\n")

    body = []
    if stdout_text:
        body.append(stdout_text)
    if stderr_text:
        body.append("--- stderr ---\n" + stderr_text)
    if not body:
        body.append("（命令没有产生任何输出）")

    notes = []
    if timed_out:
        notes.append("⚠️ 命令超时（%.1fs）已被强制终止（含子进程）；以下是终止前捕获的输出" % (timeout_ms / 1000.0))
    if over_capture:
        notes.append("⚠️ 输出超过 %dMB 采集上限，进程已被终止；结果不完整"
                     % (MAX_CAPTURE_BYTES // (1024 * 1024)))
    notes.append("退出码：%s（%d ms）" % (code if code is not None else "未知", elapsed_ms))
    if out_trunc or err_trunc:
        notes.append("… 输出超过回传预算（stdout %dKB / stderr %dKB）已截断；"
                     "建议把结果重定向到文件，再用 Read 分页查看"
                     % (LIMIT_STDOUT // 1024, LIMIT_STDERR // 1024))

    return "\n".join(body) + "\n\n" + "\n".join(notes)


def run(args):
    command = args.get("command")
    if not isinstance(command, str) or command.strip() == "":
        raise ValueError("必须提供 command（要执行的 shell 命令）")

    timeout_ms = parse_int(args, "timeout", DEFAULT_TIMEOUT_MS, MIN_TIMEOUT_MS)
    clamp_note = ""
    if timeout_ms > MAX_TIMEOUT_MS:
        clamp_note = "… timeout 已从 %d ms 收敛到上限 %d ms（执行器对脚本有 120s 硬超时）\n" % (
            timeout_ms, MAX_TIMEOUT_MS)
        timeout_ms = MAX_TIMEOUT_MS

    check_dangerous(command)

    workspace = _agent_workspace_root()
    if not workspace.is_dir():
        raise ValueError("agent 工作区不存在，无法执行命令：%s" % workspace)

    text = clamp_note + execute(command, timeout_ms, workspace)
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
        return fail("执行失败：%s" % exc)


def selftest():
    import io
    from contextlib import redirect_stdout

    def capture(args):
        buf = io.StringIO()
        with redirect_stdout(buf):
            run(args)
        return buf.getvalue()

    # 正常命令
    out = capture({"command": "echo hello"})
    assert "hello" in out and "退出码：0" in out, out

    # 非零退出码：脚本仍返回 0，输出与退出码都保留
    out = capture({"command": "echo boom >&2; exit 3"})
    assert "boom" in out and "退出码：3" in out, out

    # 超时：被杀掉并给出提示
    out = capture({"command": "sleep 3", "timeout": 1000})
    assert "超时" in out, out

    # timeout 超过上限被收敛
    out = capture({"command": "true", "timeout": 600000})
    assert "收敛到上限" in out, out

    # 危险命令拦截
    for bad in ("rm -rf /", "mkfs.ext4 /dev/sda1", "shutdown -h now", "git push --force origin main"):
        try:
            check_dangerous(bad)
        except ValueError:
            continue
        raise AssertionError("危险命令应被拦截: %s" % bad)

    # 普通命令不误杀
    for ok in ("rm -rf ./build", "rm -rf /tmp/scratch", "git push origin main", "cat README.md"):
        check_dangerous(ok)

    # 参数校验
    for bad in ({}, {"command": "   "}, {"command": "true", "timeout": 0}):
        try:
            run(bad)
        except ValueError:
            continue
        raise AssertionError("非法参数应当报错: %r" % (bad,))

    print("selftest ok")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--selftest":
        selftest()
    else:
        sys.exit(main())

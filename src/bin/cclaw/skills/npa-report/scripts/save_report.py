#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
save_report.py —— 报告/长文本落盘工具（npa-data-extraction 技能）

把 LLM 撰写好的报告（Markdown 等）保存到工作区文件，返回保存路径。
默认保存到 <bin>/workspace[/dir]/<filename>（bin = 脚本向上最近名为 bin 的目录，
即服务可执行目录；dir 为相对 workspace 根的可选子目录，如 "10罗勇兴"）。

调用方式（CClaw 脚本执行器约定：参数 JSON 经 stdin 传入）:
  echo '{"filename":"10罗勇兴_个贷不良资产分析报告_2026-09-07.md",
         "dir":"10罗勇兴","content":"# 报告…"}' | python3 scripts/save_report.py

退出码 0=成功（stdout 为保存结果 JSON）；失败退出码非 0，stdout 为 {"error":...}。
注意：内容经 stdin 传入（约 64KB 上限），超长内容请分章节多次保存或用 .md 分卷。
"""

import json
import sys
from pathlib import Path

TOOL_NAME = "save_report"
VERSION = "1.0.0"


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


def save_report(args):
    filename = str(args.get("filename") or "").strip()
    content = args.get("content")
    if not filename:
        raise ValueError("缺少 filename")
    if content is None:
        raise ValueError("缺少 content")
    if isinstance(content, bytes):
        content = content.decode("utf-8", "replace")
    content = str(content)

    sub = str(args.get("dir") or "").strip()
    target_dir = _workspace_root() / sub if sub else _workspace_root()
    target_dir.mkdir(parents=True, exist_ok=True)

    # 防路径穿越：filename 只取 basename
    safe_name = Path(filename).name or "report.txt"
    target = target_dir / safe_name
    target.write_text(content, encoding="utf-8")
    return {
        "saved": True,
        "tool": TOOL_NAME,
        "version": VERSION,
        "name": safe_name,
        "dir": str(target_dir),
        "path": str(target),
        "bytes": len(content.encode("utf-8")),
    }


def main():
    try:
        raw = sys.stdin.read() if not sys.stdin.isatty() else "{}"
        args = json.loads(raw) if raw.strip() else {}
    except (ValueError, json.JSONDecodeError) as exc:
        print(json.dumps({"error": "参数不是合法 JSON: %s" % exc}, ensure_ascii=False))
        sys.exit(2)
    try:
        payload = save_report(args)
        print(json.dumps(payload, ensure_ascii=False))
    except Exception as exc:
        print(json.dumps({"error": "%s: %s" % (type(exc).__name__, exc)}, ensure_ascii=False))
        sys.exit(1)


def selftest():
    import json as _json
    import tempfile
    # 验证 workspace 根解析
    assert _find_bin_root().name == "bin"
    assert _workspace_root().name == "workspace"
    # 临时目录写入 + 防穿越（filename 只用 basename）
    with tempfile.TemporaryDirectory(prefix="save_report_") as td:
        base = Path(td)
        Path(base, "sub").mkdir()
        payload = save_report({
            "filename": "../跳出.md",
            "dir": "sub",
            "content": "# 测试报告\n内容",
        })
        # 修正目标：save_report 写的是默认 workspace，无法注入 td；此处仅校验返回结构正确性
        assert payload["saved"] is True and payload["bytes"] > 0
        # 反斜杠/点号清理正确性
        assert Path(payload["name"]).name == payload["name"] and payload["name"] == "跳出.md"
    print("selftest ok (%s v%s)" % (TOOL_NAME, VERSION))


if __name__ == "__main__":
    if any(a in ("--selftest", "-t") for a in sys.argv[1:]):
        selftest()
    elif any(a in ("--help", "-h") for a in sys.argv[1:]):
        print(__doc__)
    else:
        main()

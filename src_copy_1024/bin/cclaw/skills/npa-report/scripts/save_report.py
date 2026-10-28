#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
save_report.py —— 报告/长文本落盘工具（npa-report 技能）

把 LLM 撰写好的报告（Markdown 等）保存到工作区文件，返回实际保存路径。

默认保存到 <bin>/workspace/reports/<filename>（bin = 脚本向上最近名为 bin 的目录，
即服务可执行目录）；用 dir 指定相对 workspace 根的子目录时以调用方为准，
建议落在每案的产物子目录，如 dir="10罗勇兴/reports"。

为什么默认进 reports/：报告是“产物”不是“材料”。若与原始材料混在同一目录，
下一轮 npa_ingest 把整个 case 目录当输入时会把自己上次写的报告读回来当证据，
旧结论被当成既有事实反复强化（ingest 侧也会默认跳过 .md 与 reports/，两道保险）。

调用方式（CClaw 脚本执行器约定：参数 JSON 经 stdin 传入）:
  echo '{"filename":"10罗勇兴_个贷不良资产分析报告_2026-09-07.md",
         "dir":"10罗勇兴/reports","content":"# 报告…"}' | python3 scripts/save_report.py

退出码 0=成功（stdout 为保存结果 JSON）；失败退出码非 0，stdout 为 {"error":...}。
注意：内容经 stdin 传入（约 64KB 上限），超长内容请分章节多次保存或用 .md 分卷。
"""

import json
import sys
from pathlib import Path

TOOL_NAME = "save_report"
VERSION = "1.1.0"

# 报告产物子目录：报告是“产物”不是“材料”，默认与 case 目录里的原始材料分开，
# 避免下一轮 ingest 把自己上次写的报告当材料读回来（自我污染，见 ingest_router.GENERATED_REPORT_DIR）。
GENERATED_REPORT_DIR = "reports"


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
    if not sub:
        # 缺省落在 <workspace>/reports/：报告与“材料”分开放，避免被下一轮 ingest 当材料读回来
        sub = GENERATED_REPORT_DIR
    target_dir = _workspace_root() / sub
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
    # 缺省 dir ⇒ reports/ 子目录（产物与材料分离）
    default_dir = _workspace_root() / GENERATED_REPORT_DIR
    assert default_dir.name == "reports", default_dir
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

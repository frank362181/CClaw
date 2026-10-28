#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
extract_metrics.py —— 结构化指标正则提取（npa-report 技能第 2 步）

读取 npa_ingest 生成的 **单个** parsed_context_*.json（含 documents[].context 按页内容），
依据 <skill>/config/extraction_rules.json 的正则规则，对每篇文档逐字段提取 11 项个贷不良
指标（姓名/性别/出生日期/身份证号/住址/发证机关/发证日期/借款合同编号/保证合同编号/
结婚状况/不动产证编号）。

结果 JSON 写 stdout（ensure_ascii=False），并默认落盘为
<bin>/temp/extracted_metrics_YYYY_MM_DD.json（stdout 顶层带 result_file 路径）。

input 契约（重要）:
  - **只能传单个文件**：上一步 npa_ingest 的 result_file.path（parsed_context json）或纯文本；
  - **不接受目录**：早期实现把目录当成"递归收集目录下所有 parsed_context 并合并"，与文档
    语义（省略 input = 取最新一份）不一致 —— 实测把 bin/temp（50 份历史产物）当输入，
    同一文件重复 8–12 次、331 条记录互相矛盾，报告因此丢掉第二份借款合同；
  - 路径丢失时**省略 input**（脚本取 bin/temp 最新产物，并在 input.used 回带实际使用的文件）。

输出里的自检字段（供调用方核对输入是否干净）:
  - `input.files`             实际消费的输入文件列表
  - `summary.documents_total` 去重前条数 / `documents_unique` 去重后条数 / `duplicates_collapsed`
  - `warnings[]`              去重、文档数过少、走了兜底等情况下的显式提醒

调用方式（两种兼容）：
  1) CLI:  python3 scripts/extract_metrics.py <parsed_context.json 或纯文本>
  2) stdin JSON（CClaw 脚本执行器约定）:  echo '{"input":"/…/parsed_context_….json"}' | python3 scripts/extract_metrics.py
"""

import json
import os
import re
import sys
from datetime import datetime
from pathlib import Path

TOOL_NAME = "extract_metrics"
VERSION = "1.2.0"

# 复用 ingest_router 的目录定位/落盘工具（同目录，避免重复实现）
import ingest_router as _ir  # noqa: E402


# ---------------------------------------------------------------------------
# 规则装载（config/extraction_rules.json）
# ---------------------------------------------------------------------------

_FLAG_MAP = {"MULTILINE": re.M, "IGNORECASE": re.I, "DOTALL": re.S}


def _rules_file():
    return _ir.Path(__file__).resolve().parent.parent / "config" / "extraction_rules.json"


def load_rules():
    """读取规则文件并预编译正则，返回 fields 列表。"""
    path = _rules_file()
    if not path.exists():
        raise FileNotFoundError("缺少规则文件: %s" % path)
    with open(path, "r", encoding="utf-8") as fh:
        data = json.load(fh)
    fields = []
    for field in data.get("fields", []):
        patterns = []
        for pat in field.get("patterns", []):
            flags = 0
            for f in pat.get("flags", []) or []:
                flags |= _FLAG_MAP.get(f, 0)
            try:
                compiled = re.compile(pat["regex"], flags)
            except re.error as exc:
                raise ValueError("规则正则有误 [%s]: %s" % (field.get("id"), exc))
            patterns.append({"compiled": compiled, **pat})
        patterns.sort(key=lambda p: p.get("priority", 99))
        fields.append({
            "id": field.get("id"),
            "name": field.get("name"),
            "strip": field.get("strip", []) or [],
            "patterns": patterns,
        })
    return fields


# ---------------------------------------------------------------------------
# 指标提取
# ---------------------------------------------------------------------------

def _capture_value(pat, m, strip_tokens):
    """按 capture/format 取出捕获值，并按 strip 清理粘连后缀（如 “（盖章）”）。"""
    cap = pat.get("capture", 1)
    if isinstance(cap, int):
        value = m.group(cap) or ""
    else:  # 多组(如日期 年/月/日)
        fmt = pat.get("format", {}) or {}
        pad = int(fmt.get("pad") or 0)
        parts = []
        for i in cap:
            g = m.group(i) or ""
            parts.append(g.zfill(pad) if pad else g)
        value = fmt.get("join", "").join(parts)

    for tok in strip_tokens:
        if value.endswith(tok):
            value = value[:-len(tok)]
    return value.strip()


def _extract_one(field, text):
    """对单个 field 按 priority 顺序提取；命中返回值字符串，否则返回 None。

    patterns[].prefer_prefix（可选，字符串数组）是**形态兜底类** pattern 的“前缀偏好”：
    同一 pattern 在一篇文档里可能命中多个值（如既有借款合同号 SJ…、又有借据号 WJ…），
    此时优先返回以偏好前缀开头的那个；都没有前缀命中时退回首个命中。
    典型用途：`source_documents` 之外的文书里，第一个形态命中往往不是合同编号。
    """
    for pat in field["patterns"]:
        prefer = [str(p).upper() for p in (pat.get("prefer_prefix") or [])]
        first_value = None
        matches = pat["compiled"].finditer(text) if prefer else (pat["compiled"].search(text),)
        for m in matches:
            if m is None:
                continue
            value = _capture_value(pat, m, field["strip"])
            if not value:
                continue
            if not prefer:
                return value
            if first_value is None:
                first_value = value
            if any(value.upper().startswith(p) for p in prefer):
                return value
        if first_value is not None:
            # 有命中但没有前缀偏好命中：按优先级语义用它（仍好于继续往更低优先级找）
            return first_value
    return None


def extract_metrics_from_text(text, fields):
    """对一段文本提取全部指标，返回 {field_id: 值或 None, ...}。"""
    return {f["id"]: _extract_one(f, text) for f in fields}


# ---------------------------------------------------------------------------
# 角色化多主体“个人信息”提取（债务人/保证人/被执行人/被告/…）
# 同一案件若有多个保证人，每人逐条列出；按 config/extraction_rules.json 的
# person_extraction 配置切分当事人块并在块内回捞身份字段。
# ---------------------------------------------------------------------------

_LABEL_LINE_RE = re.compile(r"^\s*([\u4e00-\u9fa5A-Za-z0-9（）()]{1,20})\s*[：:]\s?(.*)$")
_PAGE_SEP_RE = re.compile(r"^\s*={4,}")
_SUFFIX_RE = re.compile(r"[（(]以下简称[^）)]*[）)]")
_ROLE_RANK = {"债务人": 0, "保证人": 1, "债权人": 2, "待判定": 9}
_CROSSREF_KEYWORDS = (
    "债务", "连带", "清偿", "承担", "上述", "责任", "本金", "利息", "罚息",
    "被告", "原告", "本案", "该行", "经理", "法定", "涉案", "提供", "起诉",
)


def load_person_cfg():
    """读取 extraction_rules.json 顶层 person_extraction 配置（无则空 dict）。"""
    path = _rules_file()
    if not path.exists():
        return {}
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return (json.load(fh) or {}).get("person_extraction") or {}
    except Exception:
        return {}


def _derive_gender_birth(idno):
    """由 18 位身份证号推导 (性别, 出生日期 YYYY-MM-DD)；非 18 位返回 (None, None)。"""
    s = str(idno or "").strip().upper()
    if not re.fullmatch(r"[1-9]\d{16}[\dX]", s):
        return None, None
    gender = "男" if int(s[16]) % 2 == 1 else "女"
    birth = "%s-%s-%s" % (s[6:10], s[10:12], s[12:14])
    return gender, birth


def _is_break_label(label, pcfg):
    """标签是否属于“打断块”的辅助标签（法定代表人/负责人/诉讼代理人等）。"""
    for b in pcfg.get("break_labels", []) or []:
        if label == b or label.startswith(b) or b.startswith(label):
            return True
    return False


def _parse_party_blocks(text, pcfg):
    """按“标签+：”行把当事人(原告/被告/被执行人/借款人/保证人等)切成独立块。

    返回 [{label, role_hint, lines}]；role_hint 来自 party_block_labels 映射，
    可能是“待判定”（如被告/被执行人），待 cross-ref 语句再归位。
    """
    party_map = pcfg.get("party_block_labels", {}) or {}
    max_block = int(pcfg.get("max_block_chars", 500) or 500)
    blocks, cur = [], None
    for raw in text.splitlines():
        line = raw.strip()
        if not line or _PAGE_SEP_RE.match(line):
            if cur is not None:
                blocks.append(cur)
                cur = None
            continue
        m = _LABEL_LINE_RE.match(line)
        if m:
            label = m.group(1).strip()
            role = party_map.get(label)
            is_break = _is_break_label(label, pcfg)
            if role or is_break:
                # 新的“标签：”行结束上一块（当事人/辅助标签都是块边界）
                if cur is not None:
                    blocks.append(cur)
                    cur = None
                if role and not is_break:
                    cur = {"label": label, "role_hint": role, "lines": [line]}
            else:
                # 非当事人/辅助标签的“词：”行（如正文“本院认为：”）接续当前块
                if cur is not None:
                    cur["lines"].append(line)
            continue
        if cur is not None:
            if sum(len(l) for l in cur["lines"]) >= max_block:
                blocks.append(cur)
                cur = None
            else:
                cur["lines"].append(line)
    if cur is not None:
        blocks.append(cur)
    return blocks


def _block_name(block, pcfg):
    """从块首行“标签：姓名…”取姓名/机构名。

    取到分隔符或“（以下简称…）/对/为/向/未到庭/承担/上述”等续句处为止，
    避免把整句（如“…有限公司对罗勇兴的上述债务承担连带清偿责任”）吞进名称。
    """
    label = block["label"]
    first = (block["lines"] or [""])[0]
    m = _LABEL_LINE_RE.match(first)
    rest = (m.group(2) if m and m.group(1).strip() == label else first).strip()
    name = ""
    if rest:
        nm = re.match(
            r"^([\u4e00-\u9fa5A-Za-z0-9·（）()]{2,%d}?)"
            r"(?=[，,。；;：:、\s]|对|为|向|未到庭|经本院|承担|上述|（以下|以下简称|$)"
            % int(pcfg.get("name_max_chars", 40) or 40),
            rest,
        )
        if nm:
            name = _SUFFIX_RE.sub("", nm.group(1)).strip().strip("：: ")
    if not name or len(name) == 1:
        return None
    return name


def _cross_ref_roles(text, pcfg):
    """扫描“借款人：X、Y”“保证人：A、B”等语句 → {名称token: 业务角色}。"""
    result = {}
    for role, labels in (pcfg.get("cross_ref_labels", {}) or {}).items():
        pat = re.compile(
            r"(?:%s)\s*[：:]\s*([^。；;\n]{1,160})" % "|".join(re.escape(x) for x in labels)
        )
        for m in pat.finditer(text):
            seg = m.group(1)
            for tok in re.split(r"[、，,；;和及与]", seg):
                tok = tok.strip()
                if not (1 < len(tok) <= int(pcfg.get("name_max_chars", 40) or 40)):
                    continue
                if not re.search(r"[\u4e00-\u9fa5]", tok):
                    continue
                if any(k in tok for k in _CROSSREF_KEYWORDS):
                    continue
                result.setdefault(tok, role)
    return result


def _person_token_match(name, token):
    """判断姓名/机构名与 cross-ref token 是否同一人（兼容“喆广公司”vs全称/简称）。"""
    if not name or not token:
        return False
    if token == name or token in name or name in token:
        return True

    def core(s):
        s = re.sub(r"[市省区]", "", s)
        s = (s.replace("有限责任公司", "")
              .replace("有限公司", "")
              .replace("股份公司", "")
              .replace("公司", "")
              .replace("厂", "")
              .strip())
        return s

    a, b = core(name), core(token)
    if not a or not b or len(a) < 2 or len(b) < 2:
        return False
    return a == b or a in b or b in a


def _block_address(text):
    """块内地址兜底回捞：户籍地/住/住所地/住址等（仅用于当事人块上下文）。"""
    pats = [
        r"(?:户籍地|户籍地址|户籍所在地)\s*[：:]?\s*([^，。；;\n]{4,120})",
        r"[，,]\s*住\s*([^，。；;\n]{4,120})",
        r"(?:住所地|住所|住址|居住地址|常住地址|现住址)\s*[：:]?\s*([^，。；;\n]{4,120})",
    ]
    for pat in pats:
        m = re.search(pat, text)
        if m:
            val = (m.group(1) or "").strip().strip("：:。")
            if len(val) >= 4 and "约定送达" not in val[:6]:
                return val
    return None


def _person_from_block(block, fields, pcfg, crossref):
    """在单个人块内回捞个人基本字段；返回 person dict（无姓名则 None）。"""
    by_id = {f["id"]: f for f in fields}
    text = "\n".join(block["lines"])
    name = _block_name(block, pcfg)
    if not name:
        return None

    def pick(fid):
        f = by_id.get(fid)
        return _extract_one(f, text) if f else None

    idno = pick("id_card_no")
    gender, birth = None, None
    if idno and pcfg.get("derive_from_id", True):
        gender, birth = _derive_gender_birth(idno)
    addr = pick("address") or _block_address(text)
    person = {
        "role_label": block["label"],
        "role": block["role_hint"],  # 可能是“待判定”，下方 cross-ref 归位
        "name": name,
        "gender": pick("gender") or gender,
        "birth_date": pick("birth_date") or birth,
        "id_card_no": idno,
        "address": addr,
        "marital_status": pick("marital_status"),
        "issuing_authority": pick("issuing_authority"),
        "issue_date": pick("issue_date"),
    }
    # 用全文“借款人：X / 保证人：X、Y”语句把 被告/被执行人 归位为 债务人/保证人
    if person["role"] in ("待判定", None):
        for token, role in crossref.items():
            if role in ("债务人", "保证人") and _person_token_match(name, token):
                person["role"] = role
                person["cross_ref"] = token
                break
    return person


def extract_persons_from_text(text, fields, pcfg):
    """整篇文档 → 角色化个人信息列表（每名当事人一条；同案多保证人逐条列出）。"""
    if not pcfg:
        return []
    crossref = _cross_ref_roles(text, pcfg)
    out = []
    for block in _parse_party_blocks(text, pcfg):
        person = _person_from_block(block, fields, pcfg, crossref)
        if person:
            out.append(person)
    return out


def _merge_roster(documents):
    """把多篇文档 persons 按 身份证号/姓名 合并，输出全库个人名单 personal_info_roster。"""
    order = []  # 保序 + 支持“全称 vs 简称”同名合并
    merged = {}

    def _find_merge_target(p):
        """有身份证按身份证精确合并；否则按姓名（全称/简称兼容）找已存在条目。"""
        idno = (p.get("id_card_no") or "").upper()
        if idno and idno in merged:
            return merged[idno], idno
        if idno:
            return None, idno
        for m in merged.values():
            if m.get("name") and _person_token_match(m["name"], p.get("name")):
                return m, None
        return None, None

    for doc in documents:
        file_ = doc.get("file")
        for p in doc.get("persons") or []:
            if not (p.get("id_card_no") or p.get("name")):
                continue
            target, key = _find_merge_target(p)
            if target is None:
                target = {
                    "name": p.get("name"),
                    "id_card_no": p.get("id_card_no"),
                    "roles": [],
                    "role_labels": [],
                    "gender": None,
                    "birth_date": None,
                    "address": None,
                    "marital_status": None,
                    "issuing_authority": None,
                    "issue_date": None,
                    "source_files": [],
                }
                order.append(target)
                if key:
                    merged[key] = target
                else:
                    merged.setdefault(p.get("name"), target)
            for f in ("name", "id_card_no", "gender", "birth_date", "address",
                      "marital_status", "issuing_authority", "issue_date"):
                v = p.get(f)
                if v and not target[f]:
                    target[f] = v
            if p.get("role") and p["role"] not in target["roles"]:
                target["roles"].append(p["role"])
            if p.get("role_label") and p["role_label"] not in target["role_labels"]:
                target["role_labels"].append(p["role_label"])
            if file_ and file_ not in target["source_files"]:
                target["source_files"].append(file_)

    for m in order:
        if m["roles"]:
            roles = sorted(m["roles"], key=lambda r: _ROLE_RANK.get(r, 9))
            m["role"] = roles[0]
        else:
            m["role"] = "待判定"
    order.sort(key=lambda x: (_ROLE_RANK.get(x["role"], 9), x["name"] or ""))
    return order


def _text_of_document(doc):
    """从 npa_ingest 的 document 对象取出用于抽取的文本。"""
    context = doc.get("context")
    if isinstance(context, str):
        return context
    if isinstance(context, list):
        parts = []
        for item in context:
            if isinstance(item, dict):
                parts.append(str(item.get("content") or ""))
            else:
                parts.append(str(item))
        return "\n".join(parts)
    return str(context or "")


# ---------------------------------------------------------------------------
# 输入解析：parsed JSON / 目录 / 纯文本
# ---------------------------------------------------------------------------

def _collect_texts_from_file(path):
    """单个输入文件 → [(label, text), ...]。json 含 documents 则展开。"""
    ext = os.path.splitext(path)[1].lower()
    if ext == ".json":
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
        docs = data.get("documents") if isinstance(data, dict) else data
        if isinstance(docs, list) and docs and isinstance(docs[0], dict) and "context" in docs[0]:
            return [(d.get("file") or os.path.basename(path), _text_of_document(d)) for d in docs]
        if isinstance(data, dict) and data.get("text") is not None:
            return [(os.path.basename(path), str(data["text"]))]
    text, _ = _ir.read_text_file(path, _ir._max_text_bytes({}))
    return [(os.path.basename(path), text)]


def _dir_candidates_hint(path):
    """目录入参被拒时附上候选提示（优先列最近几轮 run 目录，其次旧的扁平产物），帮助自我纠正。"""
    try:
        root = Path(path)
        runs_root = root / _ir.RUN_DIR_NAME
        runs = []
        if runs_root.is_dir():
            runs = sorted([p.name for p in runs_root.iterdir() if p.is_dir()], reverse=True)[:5]
        if runs:
            return "；该目录下最近几轮 run（请取其下的 %s）: %s" % (
                _ir.RUN_PARSED_NAME, ", ".join(runs))
        cands = sorted(root.glob("parsed_context_*.json"),
                       key=lambda p: p.stat().st_mtime, reverse=True)[:5]
    except Exception:
        return ""
    if not cands:
        return ""
    return "；该目录下候选(最近 5 个): " + ", ".join(p.name for p in cands)


def collect_inputs(input_path):
    """input 必须是**单个文件**（parsed_context json / 纯文本）→ [(label, text)]；目录一律拒绝。

    为什么拒绝目录:目录入参曾被实现为"递归收集目录下所有 parsed_context json 并合并"，
    与文档语义("省略 input = 取 bin/temp 最新一份")不一致 —— 实测把 bin/temp(50 份历史
    产物)传进来后，同一文件重复 8–12 次、331 条记录互相矛盾，模型只能"高频者胜"，
    低频细节(如第二份借款合同 SJ…537N1)会被丢掉。宁可报错也不静默合并。
    """
    input_path = os.path.expanduser(str(input_path or "").strip())
    if not input_path or not os.path.exists(input_path):
        raise ValueError("输入不存在: %s" % input_path)
    if os.path.isfile(input_path):
        return _collect_texts_from_file(input_path)

    raise ValueError(
        "input 不接受目录(会被误当成多次运行产物的合并): %s%s。"
        "请把上一步 npa_ingest 的 result_file.path 原样传入；"
        "路径丢失时可省略 input，脚本会自动取 bin/temp 最新一份产物并在 input.used 回带"
        % (input_path, _dir_candidates_hint(input_path))
    )


# ---------------------------------------------------------------------------
# 主流程
# ---------------------------------------------------------------------------

def _doc_key(label):
    """去重键：相对路径归一化（分隔符/大小写）后比较。"""
    return os.path.normpath(str(label)).replace("\\", "/").lower()


def _doc_rank(doc):
    """保留优先级：命中字段多者优先，其次文本长者优先（视为更完整的一次解析）。"""
    return (len(doc.get("found") or []), int(doc.get("text_chars") or 0))


def _dedup_documents(documents):
    """同一文件的多条记录只保留最完整的一条，返回 (去重后列表, 被折叠条数)。

    为什么要去重：重复 ingest / 目录入参(已禁止)会把同一文件以多个版本塞进输入，
    模型面对互相矛盾的副本会"高频者胜"，低频细节(如第二份借款合同的编号)会被丢弃。
    去重后每个文件只出现一次，与报告"按文件列示"的预期一致。
    """
    best, kept_order, dropped = {}, [], 0
    for doc in documents:
        key = _doc_key(doc.get("file", ""))
        cur = best.get(key)
        if cur is None:
            best[key] = doc
            kept_order.append(key)
            continue
        dropped += 1
        if _doc_rank(doc) > _doc_rank(cur):
            best[key] = doc
    return [best[k] for k in kept_order], dropped


def _latest_parsed_file():
    """input 缺失时的兜底：优先取最新一轮 run 的 parsed_context.json，其次旧的扁平产物。

    目录入参已被拒（见 collect_inputs），所以这里只会返回**单个文件**：
    最新 run ⇒ 天然对应“最近一次 ingest”，不会把历史轮次混进来。
    """
    temp = _ir._default_result_dir()
    runs_root = temp / _ir.RUN_DIR_NAME
    if runs_root.is_dir():
        runs = sorted([p for p in runs_root.iterdir() if p.is_dir()], key=lambda p: p.name, reverse=True)
        for run in runs:
            cand = run / _ir.RUN_PARSED_NAME
            if cand.is_file():
                return cand
    if not temp.is_dir():
        return None
    cands = [p for p in temp.glob("parsed_context_*.json") if p.is_file()]
    if not cands:
        return None
    return max(cands, key=lambda p: p.stat().st_mtime)


def _run_dir_of(path):
    """输入文件若位于 <temp>/runs/<run_id>/ 下，返回该 run 目录（否则 None = 旧扁平产物）。"""
    try:
        parent = Path(path).resolve().parent
    except Exception:
        return None
    return parent if parent.parent.name == _ir.RUN_DIR_NAME else None


def _read_manifest(run_dir):
    """读取本轮材料清单（ingest 写入的 manifest.json）；没有/不可读返回 None。"""
    if run_dir is None:
        return None
    mf = run_dir / _ir.RUN_MANIFEST_NAME
    if not mf.is_file():
        return None
    try:
        with open(mf, "r", encoding="utf-8") as fh:
            data = json.load(fh)
        return data if isinstance(data, dict) else None
    except Exception:
        return None


def _materials_check(manifest, documents):
    """P1-2 对账：把本轮材料清单与抽取出的文档列表比一比，不一致就暴露出来。"""
    declared_files = [m.get("rel") for m in (manifest.get("materials") or []) if m.get("rel")]
    declared = (manifest.get("summary") or {}).get("files_processed")
    got = {d.get("file") for d in documents}
    missing = [f for f in declared_files if f not in got]
    return {
        "declared_documents": declared,
        "declared_files": len(declared_files),
        "extracted_documents": len(documents),
        "missing_files": missing[:20],
        "match": bool(declared == len(documents) and not missing),
    }


def run_extract(opts):
    fields = load_rules()
    pcfg = load_person_cfg()
    requested = str(opts.get("input") or opts.get("path") or "").strip()
    used, fallback = requested, False

    if requested:
        items = collect_inputs(requested)  # 目录/不存在都会报错
    else:
        items = []
    if not items:  # 未给 input → 兜底 bin/temp 里最新的一份产物
        latest = _latest_parsed_file()
        if latest is not None:
            items = collect_inputs(str(latest))
            used, fallback = str(latest), True
    if not items:
        raise ValueError(
            "没有可提取的输入：请先调用 npa_ingest 生成 parsed_context_*.json（bin/temp）"
            "，或显式传入 input 路径（单个 parsed_context json / 纯文本）"
        )

    documents = []
    for label, text in items:
        metrics = extract_metrics_from_text(text, fields)
        # 身份证号兜底推导/校验 性别、出生日期（标签缺失时）
        idno = metrics.get("id_card_no")
        if idno and pcfg.get("derive_from_id", True):
            g, b = _derive_gender_birth(idno)
            if not metrics.get("gender") and g:
                metrics["gender"] = g
            if not metrics.get("birth_date") and b:
                metrics["birth_date"] = b
        doc = {
            "file": label,
            "text_chars": len(text),
            "metrics": metrics,
            "found": [k for k, v in metrics.items() if v],
        }
        # 角色化多主体“个人信息”：债务人/每名保证人等（见 documents[].persons）
        if pcfg:
            try:
                persons = extract_persons_from_text(text, fields, pcfg)
            except Exception:
                persons = []
            if persons:
                doc["persons"] = persons
        documents.append(doc)

    documents_total = len(documents)
    documents, duplicates_collapsed = _dedup_documents(documents)
    total_hits = sum(len(d.get("found") or []) for d in documents)
    persons_docs = sum(1 for d in documents if d.get("persons"))

    # ---- P0-2/P1-2：识别本轮 run、读材料清单对账 ----
    run_dir = _run_dir_of(used)
    manifest = _read_manifest(run_dir)
    check = _materials_check(manifest, documents) if manifest else None
    run_id = (manifest or {}).get("run_id") or (run_dir.name if run_dir else None)

    warnings = []
    if duplicates_collapsed:
        warnings.append(
            "已折叠 %d 条重复文件记录(同一文件在输入里出现多次)，每个文件只保留最完整的一条"
            % duplicates_collapsed
        )
    if len(documents) < 2:
        warnings.append(
            "本次输入只解析出 %d 份文档，可能不是整轮 ingest 的结果 —— 请核对 input.used"
            % len(documents)
        )
    if fallback:
        warnings.append(
            "未传 input，已自动取最新一轮 run 的产物：%s（%d 份文档）" % (used, len(documents))
        )
    if manifest is None:
        warnings.append(
            "input 不在 run 目录内（旧版扁平产物），无法与材料清单对账；"
            "建议改用本轮 npa_ingest 的 result_file.path（<bin>/temp/runs/<run_id>/%s）"
            % _ir.RUN_PARSED_NAME
        )
    elif not check.get("match"):
        warnings.append(
            "与材料清单不一致：清单声明 %s 份、实际抽取 %d 份%s；请核对 input.used 是否为 %s 轮的产物"
            % (check.get("declared_documents"), len(documents),
               "（缺失: %s）" % ", ".join(check.get("missing_files") or []) if check.get("missing_files") else "",
               run_id)
        )

    payload = {
        "tool": TOOL_NAME,
        "version": VERSION,
        "rules": str(_rules_file()),
        "run_id": run_id,
        "input": {
            "requested": requested or None,
            "used": used,
            "fallback": fallback,
            "files": [used] if used else [],
        },
        "processed_at": _ir.iso_now(),
        "documents": documents,
        "summary": {
            "documents": len(documents),
            "documents_total": documents_total,
            "documents_unique": len(documents),
            "duplicates_collapsed": duplicates_collapsed,
            "fields_per_doc": len(fields),
            "total_hits": total_hits,
            "documents_with_persons": persons_docs,
            "materials_declared": (check or {}).get("declared_documents"),
            "materials_check": check,
        },
        "warnings": warnings,
    }
    if run_dir is not None:
        payload["run_dir"] = str(run_dir)
    if manifest is not None:
        payload["manifest_file"] = str(run_dir / _ir.RUN_MANIFEST_NAME)
    roster = _merge_roster(documents)
    if roster:
        payload["personal_info_roster"] = roster
    return payload


def load_args():
    """与 ingest_router 相同的双模式取参：stdin JSON > 命令行位置参数。"""
    args = {}
    try:
        raw_stdin = sys.stdin.read() if not sys.stdin.isatty() else ""
    except Exception:
        raw_stdin = ""
    if raw_stdin.strip():
        try:
            data = json.loads(raw_stdin)
            if isinstance(data, dict):
                args.update(data)
        except (ValueError, json.JSONDecodeError):
            pass
    positionals = [a for a in sys.argv[1:] if not a.startswith("-")]
    if positionals:
        args["input"] = positionals[0]
    return args


def _output_opts(payload, opts):
    """抽取结果的落盘参数：默认写回输入所在的 run 目录（extracted_metrics.json），
    否则（旧扁平产物）沿用带日期的文件名。

    覆盖旧行为只在 run 目录内生效（同一轮只应有一份抽取结果）；旧扁平产物保持
    "同名追加 _HHMMSS 不覆盖"的原有语义，避免静静盖掉当天已有产物。
    """
    merged = dict(opts)
    run_dir = payload.get("run_dir")
    if not str(merged.get("result_dir") or "").strip() and run_dir:
        merged["result_dir"] = run_dir
        merged.setdefault("overwrite_result", True)
    if not str(merged.get("result_name") or "").strip():
        merged["result_name"] = (_ir.RUN_METRICS_NAME if run_dir
                                 else "extracted_metrics_%s.json" % datetime.now().strftime("%Y_%m_%d"))
    return merged


def main():
    opts = load_args()
    try:
        payload = run_extract(opts)
    except Exception as exc:
        print(json.dumps({"error": "%s: %s" % (type(exc).__name__, exc)}, ensure_ascii=False))
        sys.exit(1)

    # 落盘：默认写回同一 run 目录（extracted_metrics.json），路径回填到 stdout 的 result_file
    result_info = _ir.save_result_file(payload, _output_opts(payload, opts))
    if result_info is not None:
        payload = dict(payload)
        payload["result_file"] = result_info
    print(json.dumps(payload, ensure_ascii=False))


# ---------------------------------------------------------------------------
# 自检
# ---------------------------------------------------------------------------

def selftest():
    fields = load_rules()
    ids = [f["id"] for f in fields]
    assert len(fields) == 11, ids
    fids = [f["id"] for f in fields]
    assert "guarantee_contract_no" in fids, fids
    sample = (
        "姓名：张三 性别：男 出生日期：1990年1月2日\n"
        "住址：浙江省杭州市西湖区文一西路969号\n"
        "公民身份号码：110101199001011234 签发机关：北京市公安局朝阳分局 签发日期：2015.01.01\n"
        "婚姻状况：已婚\n借款合同编号：GD2025-0012\n"
        "不动产权证书编号：京（2020）朝阳不动产权第0001234号\n"
    )
    metrics = extract_metrics_from_text(sample, fields)
    assert metrics["name"] == "张三"
    assert metrics["gender"] == "男"
    assert metrics["birth_date"] == "1990-01-02"
    assert metrics["id_card_no"] == "110101199001011234"
    assert metrics["issue_date"] == "2015-01-01"
    assert metrics["loan_contract_no"] == "GD2025-0012"

    # ---- 编号类字段：标签允许逐字换行(竖排版式)、值必须满足编号形态、兜底可配前缀偏好 ----
    labeled = (
        "3 、借款合同编号：SJ202108250196537 、\nSJ202108250196537N1；\n"
        "保\n证\n合\n同\n编\n号\n：\nSJ202108250196537-1\n、\nSJ202108250196537-2；\n"
    )
    m2 = extract_metrics_from_text(labeled, fields)
    assert m2["loan_contract_no"] == "SJ202108250196537", m2["loan_contract_no"]
    assert m2["guarantee_contract_no"] == "SJ202108250196537-1", m2["guarantee_contract_no"]
    # 旧规则用 [：:\s]{0,3} 跨行会把“贷款人：”当值取出
    assert extract_metrics_from_text("合同编号\n贷款人：江苏银行", fields)["loan_contract_no"] is None
    # 无标签兜底：优先取偏好前缀(SJ)，不被同文档里的借据号(WJ)截走
    fallback = "WJXD2022082911225146\n/\nSJ202108250196537N1\nWJ210914000000607\n"
    assert extract_metrics_from_text(fallback, fields)["loan_contract_no"] == "SJ202108250196537N1"
    # 仅存在非偏好前缀时退回首个命中（如借据页/截屏）
    assert extract_metrics_from_text("WJ210914000000607", fields)["loan_contract_no"] == "WJ210914000000607"
    assert metrics["marital_status"] == "已婚"
    assert metrics["real_estate_cert_no"] == "京（2020）朝阳不动产权第0001234号"
    assert metrics["address"] and "文一西路969号" in metrics["address"]
    assert metrics["issuing_authority"] == "北京市公安局朝阳分局"

    # ---- 角色化多主体“个人信息”：债务人 + 同一案多保证人逐条列出 ----
    pcfg = load_person_cfg()
    assert pcfg, "person_extraction 配置缺失"
    multi = (
        "原告：江苏银行股份有限公司常州分行，统一社会信用代码91320400704046517A，"
        "住所地江苏省常州市延陵中路500号。\n"
        "负责人：吴凌君，该行行长。\n"
        "被告：罗勇兴，男，汉族，1985年7月5日生，公民身份号码412702198507050070，"
        "户籍地河南省项城市城郊乡罗庄村2号院。\n"
        "被告：杨瑞雪，女，汉族，1988年2月20日生，公民身份号码412721198802201504，"
        "住江苏省常州市武进区大明蓝山湖苑7幢乙单元1302室。\n"
        "被告：常州市喆广金属材料有限公司，统一社会信用代码91320402061810883F，"
        "住所地江苏省常州市天宁区青洋中路1号东1517。\n"
        "被告：常州市喆广金属材料有限公司（以下简称喆广公司）对罗勇兴的上述债务承担连带清偿责任。\n"
        "\n"
        "2、借款人：罗勇兴；\n"
        "7、保证人：杨瑞雪、喆广公司；\n"
    )
    persons = extract_persons_from_text(multi, fields, pcfg)
    by_name = {}
    for p in persons:
        by_name[p["name"]] = p  # 同名人后者覆盖
    assert "罗勇兴" in by_name, persons
    assert "杨瑞雪" in by_name, persons
    assert any("喆广" in n for n in by_name), persons
    zhe = by_name.get("常州市喆广金属材料有限公司")
    assert zhe is not None, persons
    assert by_name["罗勇兴"]["role"] == "债务人", persons
    assert by_name["杨瑞雪"]["role"] == "保证人", persons
    assert zhe["role"] == "保证人", persons
    assert by_name["罗勇兴"]["id_card_no"] == "412702198507050070"
    assert by_name["罗勇兴"]["gender"] == "男"
    assert by_name["罗勇兴"]["birth_date"] == "1985-07-05"
    assert by_name["罗勇兴"]["address"] and "项城市城郊乡罗庄村" in by_name["罗勇兴"]["address"]
    assert by_name["杨瑞雪"]["id_card_no"] == "412721198802201504"
    assert by_name["杨瑞雪"]["gender"] == "女"
    assert by_name["杨瑞雪"]["address"] and "大明蓝山湖苑" in by_name["杨瑞雪"]["address"]
    assert zhe["id_card_no"] is None  # 企业以统一社会信用代码登记，非身份证

    # 汇总 roster（多文档合并）
    roster = _merge_roster([{"file": "selftest.pdf", "persons": persons}])
    rmap = {r["name"]: r for r in roster}
    assert "罗勇兴" in rmap and rmap["罗勇兴"]["role"] == "债务人", roster
    assert "杨瑞雪" in rmap and rmap["杨瑞雪"]["role"] == "保证人", roster
    assert "常州市喆广金属材料有限公司" in rmap and rmap["常州市喆广金属材料有限公司"]["role"] == "保证人", roster

    # ---- 去重：同文件多版本 → 保留最完整的一条（命中字段多 > 文本长）----
    docs = [
        {"file": "a.pdf", "text_chars": 10, "found": ["x"], "metrics": {}},
        {"file": "A.PDF", "text_chars": 99, "found": ["x", "y"], "metrics": {}},
        {"file": "b.pdf", "text_chars": 5, "found": [], "metrics": {}},
    ]
    uniq, dropped = _dedup_documents(docs)
    assert dropped == 1, dropped
    assert len(uniq) == 2, [d["file"] for d in uniq]
    assert next(d for d in uniq if d["file"] == "A.PDF")["text_chars"] == 99

    # ---- 目录入参必须被拒绝（不再静默合并历史产物）----
    import tempfile
    with tempfile.TemporaryDirectory(prefix="extract_input_") as td:
        Path(td, "parsed_context_2026_09_18.json").write_text("{}", encoding="utf-8")
        try:
            collect_inputs(td)
        except ValueError as exc:
            assert "不接受目录" in str(exc), exc
        else:
            raise AssertionError("目录入参应当报错")

    print("selftest ok (%s v%s, %d fields, persons ok)" % (TOOL_NAME, VERSION, len(fields)))


if __name__ == "__main__":
    if any(a in ("--selftest", "-t") for a in sys.argv[1:]):
        selftest()
    elif any(a in ("--help", "-h") for a in sys.argv[1:]):
        print(__doc__)
    else:
        main()

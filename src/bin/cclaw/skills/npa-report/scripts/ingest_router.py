#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
ingest_router.py —— 数据摄入路由器（npa-data-extraction 技能第一步）

从指定目录（可含子目录）或单个文件中读取 PDF / JPG 等图片 / Word / Excel /
纯文本等文档，按"页"抽取内容，组装为统一 JSON 输出给下游（结构化抽取 / LLM），
并把结果以 parsed_context_YYYY_MM_DD.json 写入 <bin>/temp 目录。

输出结构（每个文件一条 document）:
    {
      "file":     "相对路径/文件名",
      "type":     "pdf|word|excel|text|image|html|rtf",
      "context":  [ {"page": 1, "content": "...", "truncated": false, "note": ""}, ... ],
      "metadata": { "pages": 12, "chars": 3456, "format": "pdf", ... }
    }
顶层另附 documents[] 与 summary{}（总数/失败/错误清单）。

"按页"口径：
  - PDF       : 物理页(page)；
  - JPG等图片 : 每张图片计为 1 页。默认不做 OCR（避免对照片/logo 等盲目识别）：
                仅登记尺寸并附“OCR 未启用”提示；需要识别文字时显式传 ocr=true，
                由可用引擎(rapidocr_onnxruntime > paddleocr > 系统 tesseract)识别并填入 content；
  - Excel     : 每个工作表(sheet)计为 1 页；
  - Word      : 显式分页符切页；无分页符时整篇为 1 页（Word 分页由渲染决定），
                可设 word_paragraphs_per_page=N 按段落切"虚拟页"；
  - 纯文本    : 整篇为 1 页。

调用方式（两种兼容）：
  1) CLI（SKILL.md 方式）:  python3 scripts/ingest_router.py <文件或目录路径>
  2) stdin JSON（CClaw 脚本执行器约定，参数经 stdin 传入）:
        echo '{"path": "/xxx/yyy"}' | python3 scripts/ingest_router.py
  结果 JSON 写 stdout（ensure_ascii=False），退出码 0=成功；
  失败时退出码非 0，stdout 为 {"error": "..."}，stderr 为详细原因。

可选参数（stdin JSON 或与路径并列的 JSON，见 load_args）:
    path                  必填，文件或目录路径
    recursive             true 表示递归子目录（默认 true）
    include_hidden        false 表示跳过隐藏文件/目录（默认 false）
    extensions            [] 限定扩展名，如 [".pdf",".jpg",".docx"]（空=全部支持类型）
    max_files             最多处理文件数（0=不限）
    max_pages_per_file    每文件最多输出页数（0=不限）
    max_chars_per_page    每页最多字符数（0=不限，超出以 …[内容已截断] 标注）
    max_chars_per_file    每文件累计字符上限（0=不限）
    max_text_mb           纯文本/rtf 整读文件大小上限（MB，默认 64，防超大文件 OOM）
    word_paragraphs_per_page  docx 按 N 段切虚拟页（0=关）
    context_style         "pages"=按页列表（默认）| "text"=整段拼接文本
    workers               并行处理文件数（0=顺序执行，默认 0）
    ocr                   false=关闭(默认)；true=对 jpg 等图片/扫描 PDF 空页启用 OCR
    ocr_langs             tesseract 语言，如 "chi_sim+eng"（rapidocr 内置中文，默认用不到）
    ocr_dpi               扫描 PDF 页渲染 OCR 的分辨率（默认 200）
    save_result           是否把结果 JSON 落盘到文件（默认 true）
    result_dir            结果输出目录（默认自动定位到 bin/temp；可覆盖为任意目录）
    result_name           结果文件名（默认 parsed_context_YYYY_MM_DD.json）
    overwrite_result      当日结果文件已存在时是否覆盖（默认 false：追加 _HHMMSS 防覆盖）
    emit_stdout           true=stdout 输出完整结果 JSON（默认）；false=stdout 仅回执保存路径/概要

结果落盘：
  - 抽取完成后，结果 JSON（ensure_ascii=False，indent=2）写入
    <result_dir>/parsed_context_YYYY_MM_DD.json，目录不存在会自动创建；
    stdout 顶层附带 result_file 字段说明保存位置。
  - result_dir 缺省时自动取"脚本向上最近一级名为 bin 的目录"下的 temp（即 bin/temp），
    与 agent 运行时目录结构无关，可从任意 cwd 调用。
  - emit_stdout=false 适合"只要文件、不回传超长 JSON"的场景（如 CClaw 脚本执行器有
    256KB 输出上限时），此时 stdout 输出轻量回执。

依赖与兜底（无强制第三方依赖，逐级降级）：
  - PDF   : pymupdf(PyMuPDF) > pypdf > pdfplumber > 系统 pdftotext；均缺则报错提示
  - Word  : docx 自带 zip+XML 解析（无依赖）；doc(旧版二进制) 用 textutil/antiword/catdoc
  - Excel : openpyxl > 自带 zip+XML 兜底；xls(旧版) 需要 xlrd
  - 图片  : Pillow 取尺寸（可选）；OCR 引擎优先 rapidocr_onnxruntime(离线中文) > paddleocr > 系统 tesseract
  - rtf   : textutil（macOS）> 原样当文本
  注意：超大文件/大批次时请用上面的 *_per_* 上限参数，避免输出超长或内存占用过高。

性能说明：
  - 文件发现按扩展名过滤、跳过隐藏/临时文件，输出顺序稳定；
  - 各抽取器均为流式/单遍处理；openpyxl 使用 read_only 模式；
  - 每文件相互独立，workers>0 时并行解析并保序汇总。
"""

import json
import os
import re
import shutil
import subprocess
import sys
import threading
import zipfile
import xml.etree.ElementTree as ET
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from datetime import datetime, timezone
from html import unescape as html_unescape
from importlib import import_module
from pathlib import Path

VERSION = "1.2.0"
TOOL_NAME = "ingest_router"

# 输出文本被截断时追加的标记（便于下游感知数据不完整）
TRUNCATED_MARK = "\n…[内容已截断]"

# ---- 结果落盘（bin/temp）默认值 ----
RESULT_DIR_NAME = "temp"                        # 输出目录名（bin 下的 temp）
RESULT_NAME_FMT = "parsed_context_%Y_%m_%d.json"  # 例如 parsed_context_2026_09_07.json
RESULT_ALT_SUFFIX_FMT = "_%H%M%S"               # 同名已存在时的防覆盖后缀

# ---- WordprocessingML / SpreadsheetML / 关系 命名空间 ----
W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
S_NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
PKG_REL_NS = "http://schemas.openxmlformats.org/package/2006/relationships"


def wq(tag):
    """构造 WordprocessingML 限定名。"""
    return "{%s}%s" % (W_NS, tag)


def sq(tag):
    """构造 SpreadsheetML 限定名。"""
    return "{%s}%s" % (S_NS, tag)


# ---- 支持的文件类型：扩展名 -> 内容形态(type) ----
# 扫描时只收下表中的文件；扩展名大小写不敏感。
EXT_TYPE = {
    ".pdf": "pdf",
    ".docx": "word", ".doc": "word",
    ".xlsx": "excel", ".xlsm": "excel", ".xls": "excel",
    ".jpg": "image", ".jpeg": "image", ".png": "image",
    ".bmp": "image", ".tif": "image", ".tiff": "image", ".webp": "image",
    ".csv": "text", ".txt": "text", ".md": "text", ".markdown": "text",
    ".log": "text", ".json": "text", ".xml": "text", ".sql": "text",
    ".htm": "html", ".html": "html",
    ".rtf": "rtf",
}

# 忽略的系统/临时/隐藏文件（不参与摄入）
_JUNK_NAMES = {".DS_Store", "Thumbs.db", "desktop.ini", ".~lock", "~$"}

DEFAULT_MAX_TEXT_MB = 64
# 超大纯文本文件读取上限（字节），防止一次性载入 OOM
_MAX_TEXT_BYTES = DEFAULT_MAX_TEXT_MB * 1024 * 1024

# 图片 OCR：rapidocr_onnxruntime(离线中文) 内置中文，无需语言包；以下仅供 tesseract 分支使用
DEFAULT_OCR_LANGS = "chi_sim+eng"
# 扫描 PDF 页渲染为图片再 OCR 的分辨率（默认 200dpi）
OCR_DPI = 200

# docx 中需要忽略的字段代码类节点（避免把域代码/修订删除文本混入正文）
_DOCX_IGNORE_TAGS = {
    wq("instrText"), wq("delInstrText"), wq("delText"), wq("fldChar"),
    wq("softHyphen"), wq("proofErr"), wq("bookmarkStart"), wq("bookmarkEnd"),
}


@dataclass
class Page:
    """单个"页/工作表/图片"的内容。"""
    number: int
    content: str
    truncated: bool = False
    note: str = ""

    def to_dict(self):
        return {
            "page": self.number,
            "content": self.content,
            "truncated": self.truncated,
            "note": self.note,
        }


# ---------------------------------------------------------------------------
# 基础工具
# ---------------------------------------------------------------------------

def iso_now():
    return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def _decode_bytes(data):
    """按 utf-8/gbk/big5/latin-1 顺序尝试解码，确保中文文档尽量不乱码。"""
    for enc in ("utf-8", "gbk", "big5", "latin-1"):
        try:
            return data.decode(enc)
        except (UnicodeDecodeError, ValueError):
            continue
    return data.decode("utf-8", "replace")  # latin-1 兜底不会走到这里


def read_text_file(path, max_bytes):
    """读取文本类文件；超过 max_bytes 时截断并标注，防止超大文件撑爆内存。"""
    with open(path, "rb") as fh:
        data = fh.read(max_bytes + 1)
    truncated = len(data) > max_bytes
    text = _decode_bytes(data[:max_bytes])
    if truncated:
        text += TRUNCATED_MARK
    return text, truncated


def strip_html_to_text(html):
    """极简 HTML→文本：去脚本/样式、块级标签换行、剥标签、解实体。"""
    text = re.sub(r"(?is)<(script|style|head|title|meta|link)[^>]*>.*?</\1>", " ", html)
    text = re.sub(r"(?i)<br\s*/?>", "\n", text)
    text = re.sub(r"(?i)</(p|div|tr|li|h[1-6]|table|ul|ol)>", "\n", text)
    text = re.sub(r"(?i)<td[^>]*>", " | ", text)
    text = re.sub(r"(?s)<[^>]+>", "", text)
    text = html_unescape(text)
    lines = [ln.strip() for ln in text.splitlines()]
    lines = [ln for ln in lines if ln]
    return "\n".join(lines)


def _run_cmd(argv, timeout=60):
    """执行外部命令并返回 stdout 文本；失败抛 RuntimeError。"""
    try:
        proc = subprocess.run(argv, capture_output=True, timeout=timeout)
    except FileNotFoundError as exc:
        raise RuntimeError("命令不存在: %s" % argv[0]) from exc
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError("命令超时: %s" % argv[0]) from exc
    if proc.returncode != 0:
        detail = proc.stderr.decode("utf-8", "replace").strip() or "exit=%d" % proc.returncode
        raise RuntimeError("命令失败 %s: %s" % (" ".join(argv), detail))
    return proc.stdout.decode("utf-8", "replace")


def _col_to_index(ref):
    """把电子表格单元格引用(如 'AB12')转为 0 基列号。"""
    letters = "".join(ch for ch in ref if ch.isalpha()).upper()
    idx = 0
    for ch in letters:
        idx = idx * 26 + (ord(ch) - 64)
    return idx - 1


# ---------------------------------------------------------------------------
# PDF
# ---------------------------------------------------------------------------

def _import_pdf_backend():
    """按优先级返回可用的 PDF 抽取后端名称；全部缺失返回 None。"""
    for name, module in (("pymupdf", "pymupdf"), ("pymupdf", "fitz"),
                         ("pypdf", "pypdf"), ("pdfplumber", "pdfplumber")):
        try:
            __import__(module)
            return name
        except ImportError:
            continue
    if shutil.which("pdftotext"):
        return "pdftotext"
    return None


def _extract_pdf(path, backend):
    """按物理页抽取 PDF 文本，返回 list[Page]。"""
    pages = []
    if backend == "pymupdf":
        try:
            import pymupdf
        except ImportError:  # 老版本 API 名
            import fitz as pymupdf
        doc = pymupdf.open(path)
        try:
            for i, page in enumerate(doc, start=1):
                pages.append(Page(i, page.get_text("text")))
        finally:
            doc.close()
    elif backend == "pypdf":
        pypdf = import_module("pypdf")  # 可选依赖，已由 _import_pdf_backend 探测
        reader = pypdf.PdfReader(path)
        for i, page in enumerate(reader.pages, start=1):
            pages.append(Page(i, page.extract_text() or ""))
    elif backend == "pdfplumber":
        pdfplumber = import_module("pdfplumber")
        with pdfplumber.open(path) as pdf:
            for i, page in enumerate(pdf.pages, start=1):
                pages.append(Page(i, page.extract_text() or ""))
    elif backend == "pdftotext":
        # pdftotext 用换页符(\f)分隔各页，单进程一次取出再切分，性能好
        text = _run_cmd(["pdftotext", "-layout", str(path), "-"])
        chunks = text.split("\f")
        for i, chunk in enumerate(chunks, start=1):
            pages.append(Page(i, chunk.strip("\n")))
    else:
        raise RuntimeError("缺少 PDF 解析库，请安装 pymupdf/pypdf/pdfplumber 或 pdftotext")
    return pages


def extract_pdf_pages(path, opts=None):
    """按物理页抽取 PDF 文本；空文本页(扫描件)在 pymupdf 下渲染并 OCR 回填。"""
    opts = opts or {}
    backend = _import_pdf_backend()
    pages = _extract_pdf(path, backend)
    empty = [p for p in pages if not p.content.strip()]
    if empty and _ocr_requested(opts) and _ocr_engine() is not None and backend == "pymupdf":
        try:
            import pymupdf
            zoom = max(1.0, int(opts.get("ocr_dpi") or OCR_DPI) / 72.0)
            doc = pymupdf.open(path)
            try:
                for pg in empty:
                    pix = doc[pg.number - 1].get_pixmap(matrix=pymupdf.Matrix(zoom, zoom))
                    text = _ocr_predict_text(_pixmap_to_ndarray(pix), opts).strip()
                    if text:
                        pg.content = text
                        pg.note = "扫描页 OCR(引擎=%s)" % _ocr_engine()
            finally:
                doc.close()
        except Exception:
            pass
    for pg in pages:
        if not pg.content.strip():
            if not _ocr_requested(opts):
                pg.note = "本页无可提取文本(扫描/图片页)，OCR 未启用（需要识别请传 ocr=true）"
            elif _ocr_engine() is None:
                pg.note = "本页无可提取文本；本机无 OCR 引擎(rapidocr_onnxruntime/paddleocr/tesseract)，扫描页文字未识别"
            else:
                pg.note = "本页无可提取文本；扫描页 OCR 无识别结果"
    return pages


# ---------------------------------------------------------------------------
# Word (.docx) —— 无依赖 zip + XML 单遍解析
# ---------------------------------------------------------------------------

def _docx_run_tokens(p_el):
    """收集一个 <w:p> 内的叶子 token：('t',文本) 或 ('pb',None=分页符)。"""
    for node in p_el.iter():
        tag = node.tag
        if tag in _DOCX_IGNORE_TAGS:
            continue
        if tag == wq("t"):
            yield ("t", node.text or "")
        elif tag == wq("tab"):
            yield ("t", "\t")
        elif tag == wq("cr") or tag == wq("br"):
            # br 的 type=page 才是分页符；普通 br/cr 为换行
            if tag == wq("br") and node.get(wq("type")) == "page":
                yield ("pb", None)
            else:
                yield ("t", "\n")
        elif tag == wq("lastRenderedPageBreak"):
            yield ("pb", None)
        elif tag == wq("noBreakHyphen"):
            yield ("t", "-")


def _docx_para_chunks(p_el):
    """把段落按显式分页符切成若干文本块；块之间即分页边界。"""
    chunks = []
    buf = []
    for kind, val in _docx_run_tokens(p_el):
        if kind == "t":
            buf.append(val)
        else:  # 分页符：落盘当前块，开启新块
            chunks.append("".join(buf))
            buf = []
    chunks.append("".join(buf))
    return chunks


def _docx_table_text(tbl):
    """把表格渲染为多行文本，列间用 ' | ' 分隔。"""
    lines = []
    for tr in tbl.findall(wq("tr")):
        cells = []
        for tc in tr.findall(wq("tc")):
            parts = []
            for p in tc.iter(wq("p")):
                text = "".join(v for k, v in _docx_run_tokens(p) if k == "t").strip()
                if text:
                    parts.append(text)
            cells.append("\n".join(parts))
        line = " | ".join(cells)
        if line.strip():
            lines.append(line)
    return lines


def _docx_blocks(el):
    """深度优先产出正文块：('p', 段落) 或 ('tbl', 表格)。"""
    if el.tag == wq("p"):
        yield ("p", el)
        return
    if el.tag == wq("tbl"):
        yield ("tbl", el)
        return
    for child in el:
        yield from _docx_blocks(child)


def _docx_extract_pages(abs_path, paragraphs_per_page):
    """抽取 docx 文本，按分页符/虚拟段切页。返回 list[Page]。"""
    try:
        with zipfile.ZipFile(abs_path) as zf:
            if "word/document.xml" not in zf.namelist():
                raise RuntimeError("不是有效的 docx（缺少 word/document.xml）")
            xml_data = zf.read("word/document.xml")
    except zipfile.BadZipFile as exc:
        raise RuntimeError("文件不是有效的 docx zip: %s" % exc) from exc

    root = ET.fromstring(xml_data)
    body = root.find(wq("body"))
    if body is None:
        raise RuntimeError("docx 结构异常：未找到正文")

    pages = [""]            # pages[i] 为第 i+1 页已累积文本
    para_index = 0          # 供 word_paragraphs_per_page 切虚拟页

    def ensure_page():
        if not pages:
            pages.append("")

    def push(text):
        ensure_page()
        if text:
            if pages[-1]:
                pages[-1] += "\n"
            pages[-1] += text

    def new_page():
        pages.append("")

    for block_type, block in _docx_blocks(body):
        if block_type == "p":
            # 虚拟分页：每 N 个段落开新页（在段落之前判断）
            if paragraphs_per_page > 0:
                if para_index and para_index % paragraphs_per_page == 0:
                    new_page()
                para_index += 1
                text = "".join(v for k, v in _docx_run_tokens(block) if k == "t").strip()
                push(text)
                continue

            chunks = _docx_para_chunks(block)
            for idx, chunk in enumerate(chunks):
                if idx > 0:                     # 每个分页符后开新页
                    new_page()
                chunk = chunk.strip()
                if chunk:
                    if pages[-1]:
                        pages[-1] += "\n"
                    pages[-1] += chunk
        else:  # tbl
            if paragraphs_per_page > 0:
                para_index += 1
            for line in _docx_table_text(block):
                push(line)

    # 去掉末尾因"段落以分页符结尾"产生的空页
    while pages and not pages[-1].strip():
        pages.pop()
    if not pages:
        pages = [""]

    result = [Page(i + 1, text.strip("\n")) for i, text in enumerate(pages)]
    if paragraphs_per_page <= 0 and len(result) == 1:
        result[0].note = "Word 无物理分页；未发现显式分页符，整篇作为 1 页输出"
    return result


def extract_docx_pages(abs_path, paragraphs_per_page):
    try:
        return _docx_extract_pages(abs_path, paragraphs_per_page)
    except ET.ParseError as exc:
        raise RuntimeError("docx XML 解析失败: %s" % exc) from exc


# ---------------------------------------------------------------------------
# Excel (.xlsx/.xlsm) —— openpyxl(流式) 优先，zip+XML 兜底
# ---------------------------------------------------------------------------

def _cell_to_text(value):
    """单元格值 → 文本（整数化、日期转 ISO）。"""
    if value is None:
        return ""
    if isinstance(value, bool):
        return "TRUE" if value else "FALSE"
    if isinstance(value, float):
        return str(int(value)) if value.is_integer() else str(value)
    if isinstance(value, (datetime,)):
        return value.isoformat(sep=" ", timespec="seconds")
    return str(value)


def _extract_xlsx_openpyxl(abs_path):
    """openpyxl read_only 流式读工作表。每个 sheet 计为一页。"""
    from openpyxl import load_workbook
    wb = load_workbook(abs_path, read_only=True, data_only=True)
    pages = []
    try:
        for ws in wb.worksheets:
            lines = []
            for row in ws.iter_rows(values_only=True):
                vals = [_cell_to_text(v) for v in row]
                while vals and vals[-1] == "":
                    vals.pop()
                if vals:
                    lines.append(" | ".join(vals))
            header = "工作表: %s" % ws.title
            body = "\n".join(lines)
            pages.append(Page(0, header + ("\n" + body if body else "")))
    finally:
        wb.close()
    return pages


def _xlsx_read_shared_strings(zf):
    """读取 xl/sharedStrings.xml → list[str]。"""
    if "xl/sharedStrings.xml" not in zf.namelist():
        return []
    root = ET.fromstring(zf.read("xl/sharedStrings.xml"))
    out = []
    for si in root.findall(sq("si")):
        out.append("".join(node.text or "" for node in si.iter(sq("t"))))
    return out


def _xlsx_sheet_targets(zf):
    """返回 [(sheet名, sheetXML路径), ...]（按 workbook 顺序）。"""
    wb_root = ET.fromstring(zf.read("xl/workbook.xml"))
    rels = ET.fromstring(zf.read("xl/_rels/workbook.xml.rels"))
    rel_map = {}
    rel_ns = "{%s}" % PKG_REL_NS
    for rel in rels:
        rid = rel.get("Id")
        target = rel.get("Target")
        if rid and target:
            rel_map[rid] = target.lstrip("/")
    sheets = []
    for sh in wb_root.find(sq("sheets")):
        rid = sh.get(rel_ns + "id") or sh.get("{%s}id" % R_NS)
        target = rel_map.get(rid, "")
        if target and not target.lower().startswith("xl/"):
            target = "xl/" + target
        name = sh.get("name", "Sheet")
        sheets.append((name, target))
    return sheets


def _xlsx_row_values(row, shared):
    """把一行 <row> 的单元格按列号填好 → list[str]（含空列占位）。"""
    cols = {}
    max_col = -1
    for c in row.findall(sq("c")):
        ref = c.get("r", "")
        col = _col_to_index(ref) if ref else max_col + 1
        t = c.get("t")
        value = ""
        v_el = c.find(sq("v"))
        if t == "s" and v_el is not None and v_el.text is not None:
            value = shared[int(v_el.text)] if int(v_el.text) < len(shared) else ""
        elif t == "inlineStr":
            is_el = c.find(sq("is"))
            if is_el is not None:
                value = "".join(n.text or "" for n in is_el.iter(sq("t")))
        elif t == "b":
            value = "TRUE" if (v_el is not None and v_el.text == "1") else "FALSE"
        elif v_el is not None:
            value = v_el.text or ""
        cols[col] = value
        max_col = max(max_col, col)
    return [cols.get(i, "") for i in range(max_col + 1)]


def _extract_xlsx_zip(abs_path):
    """无 openpyxl 时用 zip+XML 兜底解析。每个 sheet 计为一页。"""
    with zipfile.ZipFile(abs_path) as zf:
        shared = _xlsx_read_shared_strings(zf)
        pages = []
        for name, target in _xlsx_sheet_targets(zf):
            if not target or target not in zf.namelist():
                pages.append(Page(0, "工作表: %s（无法读取，目标缺失: %s）" % (name, target)))
                continue
            root = ET.fromstring(zf.read(target))
            lines = []
            sheet_data = root.find(sq("sheetData"))
            if sheet_data is not None:
                for row in sheet_data.findall(sq("row")):
                    vals = _xlsx_row_values(row, shared)
                    while vals and vals[-1] == "":
                        vals.pop()
                    if vals:
                        lines.append(" | ".join(vals))
            header = "工作表: %s" % name
            body = "\n".join(lines)
            pages.append(Page(0, header + ("\n" + body if body else "")))
    return pages


def extract_xlsx_pages(abs_path):
    try:
        import_module("openpyxl")
        pages = _extract_xlsx_openpyxl(abs_path)
    except ImportError:
        pages = _extract_xlsx_zip(abs_path)
    except Exception as exc:
        # openpyxl 失败(加密/损坏等)时回退到自研解析；再失败则抛原始错误
        try:
            pages = _extract_xlsx_zip(abs_path)
        except Exception:
            raise RuntimeError("xlsx 解析失败: %s" % exc) from exc
    for i, pg in enumerate(pages, start=1):
        pg.number = i
    return pages


# ---------------------------------------------------------------------------
# 旧版 Word/Excel (.doc/.xls) 与 rtf —— 借助系统工具/可选库
# ---------------------------------------------------------------------------

def _convert_via_textutil(abs_path):
    """macOS textutil：把 doc/docx/rtf/html 等转成纯文本（无分页概念）。"""
    return _run_cmd(["textutil", "-convert", "txt", "-stdout", abs_path])


def extract_doc_legacy_pages(abs_path):
    """旧版 .doc（OLE 二进制）→ 整篇文本作为 1 页。"""
    text = None
    if shutil.which("textutil"):      # macOS 自带
        text = _convert_via_textutil(abs_path)
    elif shutil.which("antiword"):
        text = _run_cmd(["antiword", abs_path])
    elif shutil.which("catdoc"):
        text = _run_cmd(["catdoc", abs_path])
    if text is None:
        raise RuntimeError(".doc 需 textutil/antiword/catdoc 之一，或另存为 .docx")
    page = Page(1, text.strip())
    page.note = "旧版 .doc 无分页信息，整篇作为 1 页输出"
    return [page]


def extract_xls_legacy_pages(abs_path):
    """旧版 .xls（BIFF 二进制）→ 需要 xlrd；每个 sheet 计为一页。"""
    try:
        xlrd = import_module("xlrd")
    except ImportError as exc:
        raise RuntimeError(".xls 需要安装 xlrd，或另存为 .xlsx") from exc
    book = xlrd.open_workbook(abs_path)
    pages = []
    for i, sheet in enumerate(book.sheets(), start=1):
        lines = []
        for r in range(sheet.nrows):
            vals = [_cell_to_text(v) for v in sheet.row_values(r)]
            while vals and vals[-1] == "":
                vals.pop()
            if vals:
                lines.append(" | ".join(vals))
        pages.append(Page(i, "工作表: %s\n%s" % (sheet.name, "\n".join(lines))))
    return pages


def extract_rtf_pages(abs_path, max_bytes):
    """rtf：textutil 转纯文本；否则原样文本（含控制符，质量差）。"""
    if shutil.which("textutil"):
        try:
            return [Page(1, _convert_via_textutil(abs_path).strip())]
        except RuntimeError:
            pass
    text, _ = read_text_file(abs_path, max_bytes)
    page = Page(1, text)
    page.note = "无 textutil，按原始文本输出（可能含 RTF 控制符）"
    return [page]


# ---------------------------------------------------------------------------
# 图片 OCR (jpg/jpeg/png/bmp/tif/webp) 与扫描 PDF 页 —— 每图 1 页
# 引擎优先级: rapidocr_onnxruntime(离线,中文) > paddleocr > 系统 tesseract
# ---------------------------------------------------------------------------

_OCR_AVAILABLE_CACHE = {}
_rapid_ocr_instance = None
_OCR_LOCK = threading.Lock()


def _module_ok(name):
    """模块是否可导入（结果缓存）。"""
    if name not in _OCR_AVAILABLE_CACHE:
        try:
            import_module(name)
            _OCR_AVAILABLE_CACHE[name] = True
        except Exception:
            _OCR_AVAILABLE_CACHE[name] = False
    return _OCR_AVAILABLE_CACHE[name]


def _ocr_requested(opts):
    """是否启用 OCR：默认关闭；仅显式 ocr=true/"true"/"1"/"yes"/"on" 时开启。"""
    v = opts.get("ocr", False)
    if isinstance(v, bool):
        return v
    return str(v).strip().lower() in ("true", "1", "yes", "on", "auto")


def _tesseract_bin():
    """系统 tesseract 可执行路径；未安装返回 None。"""
    return shutil.which("tesseract")


def _ocr_engine():
    """返回可用 OCR 引擎名：rapidocr / paddleocr / tesseract；都不可用返回 None。"""
    if _module_ok("rapidocr_onnxruntime"):
        return "rapidocr"
    if _module_ok("paddleocr"):
        return "paddleocr"
    if _tesseract_bin():
        return "tesseract"
    return None


def _to_ndarray(image):
    """bytes/ndarray → ndarray（rapidocr/paddleocr 入参）；path 原样返回。"""
    if isinstance(image, (bytes, bytearray)):
        import io
        from PIL import Image
        import numpy as np
        return np.asarray(Image.open(io.BytesIO(bytes(image))).convert("RGB"))
    return image


def _pixmap_to_ndarray(pix):
    """PyMuPDF Pixmap → BGR/RGB ndarray（OCR 用，避免依赖 PIL 编解码）。"""
    import numpy as np
    if pix.n >= 4:  # 带 alpha：转 RGB
        import pymupdf
        pix = pymupdf.Pixmap(pymupdf.csRGB, pix)
    return np.frombuffer(pix.samples, dtype=np.uint8).reshape(pix.height, pix.width, pix.n)


def _to_temp_png(image):
    """bytes/ndarray 落为临时 png 路径（paddleocr/tesseract 需要文件），返回 (path, 是否临时)。"""
    import tempfile
    from PIL import Image
    if isinstance(image, (str, os.PathLike)):
        return str(image), False
    arr = _to_ndarray(image)
    fd, path = tempfile.mkstemp(suffix=".png")
    os.close(fd)
    Image.fromarray(arr).save(path)
    return path, True


def _rapid_predict(image):
    """rapidocr_onnxruntime（离线、内置中英文 PP-OCR）。"""
    global _rapid_ocr_instance
    with _OCR_LOCK:
        if _rapid_ocr_instance is None:
            from rapidocr_onnxruntime import RapidOCR
            _rapid_ocr_instance = RapidOCR()
        try:
            result, _ = _rapid_ocr_instance(_to_ndarray(image))
        except Exception:
            return ""
    lines = [str(t).strip() for _b, t, _s in (result or []) if str(t).strip()]
    return "\n".join(lines)


def _paddle_predict(image):
    """paddleocr（若已安装）。"""
    path, tmp = _to_temp_png(image)
    try:
        from paddleocr import PaddleOCR
        with _OCR_LOCK:
            ocr = PaddleOCR(use_angle_cls=True, lang="ch", show_log=False)
            result = ocr.ocr(path, cls=True)
        lines = []
        for res in result or []:
            for item in res or []:
                if item and len(item) > 1 and item[1]:
                    lines.append(str(item[1][0]).strip())
        return "\n".join(l for l in lines if l)
    except Exception:
        return ""
    finally:
        if tmp and os.path.exists(path):
            try:
                os.remove(path)
            except OSError:
                pass


def _tesseract_predict(image, langs):
    """系统 tesseract CLI（语言包自动降级 eng）。"""
    path, tmp = _to_temp_png(image)
    try:
        binary = _tesseract_bin()
        if not binary:
            return ""
        try:
            avail = set(_run_cmd([binary, "--list-langs"]).split())
        except RuntimeError:
            avail = set()
        requested = [l.strip() for l in langs.split("+") if l.strip()]
        use = [l for l in requested if l in avail] or (["eng"] if "eng" in avail else [])
        if not use:
            return ""
        return _run_cmd([binary, path, "stdout", "-l", "+".join(use), "--psm", "3"]).strip()
    finally:
        if tmp and os.path.exists(path):
            try:
                os.remove(path)
            except OSError:
                pass


def _ocr_predict_text(image, opts=None):
    """对图片(path/bytes/ndarray)做 OCR 返回文本；引擎不可用/失败返回 ''。"""
    langs = str((opts or {}).get("ocr_langs") or DEFAULT_OCR_LANGS)
    engine = _ocr_engine()
    if engine == "rapidocr":
        return _rapid_predict(image)
    if engine == "paddleocr":
        return _paddle_predict(image)
    if engine == "tesseract":
        return _tesseract_predict(image, langs)
    return ""


def _image_size(abs_path):
    """读取图片宽高 (w,h)；Pillow 优先，其次 pymupdf；均失败返回 None。"""
    try:
        from PIL import Image
        with Image.open(abs_path) as im:
            return im.size
    except Exception:
        pass
    try:
        import pymupdf
        with pymupdf.open(abs_path) as doc:
            info = doc[0].get_image_info()
            if info:
                return (info[0].get("width"), info[0].get("height"))
    except Exception:
        pass
    return None


def extract_image_pages(abs_path, opts):
    """图片 → 单页。默认不 OCR（避免对照片/logo 盲目识别）；显式 ocr=true 且引擎可用才识别。"""
    do_ocr = _ocr_requested(opts) and _ocr_engine() is not None
    text = _ocr_predict_text(abs_path, opts) if do_ocr else ""

    page = Page(1, text.strip())
    notes = []
    wh = _image_size(abs_path)
    if wh and wh[0] and wh[1]:
        notes.append("尺寸 %dx%d" % wh)
    if page.content:
        notes.append("OCR 文本(引擎=%s)" % _ocr_engine())
    else:
        notes.append("无内嵌文本")
        if not _ocr_requested(opts):
            notes.append("OCR 未启用（需要识别请传 ocr=true）")
        elif _ocr_engine() is None:
            notes.append("本机无 OCR 引擎(rapidocr_onnxruntime/paddleocr/tesseract)")
        else:
            notes.append("OCR 无识别结果")
    page.note = "，".join(notes)
    return [page]


# ---------------------------------------------------------------------------
# 纯文本类
# ---------------------------------------------------------------------------

def extract_plain_pages(abs_path, max_bytes, is_html=False):
    text, truncated = read_text_file(abs_path, max_bytes)
    if is_html:
        text = strip_html_to_text(text) if text else ""
    page = Page(1, text, truncated=truncated)
    return [page]


# ---------------------------------------------------------------------------
# 统一抽取入口：扩展名 -> 抽取函数
# ---------------------------------------------------------------------------

def extract_pages_by_ext(abs_path, ext, opts):
    """根据扩展名分发到对应抽取器，返回 list[Page]。"""
    kind = EXT_TYPE.get(ext, "text")
    if kind == "pdf":
        return extract_pdf_pages(abs_path, opts)
    if kind == "image":
        return extract_image_pages(abs_path, opts)
    if kind == "word":
        if ext == ".docx":
            return extract_docx_pages(abs_path, int(opts.get("word_paragraphs_per_page") or 0))
        return extract_doc_legacy_pages(abs_path)
    if kind == "excel":
        if ext in (".xlsx", ".xlsm"):
            return extract_xlsx_pages(abs_path)
        return extract_xls_legacy_pages(abs_path)
    if ext == ".rtf":
        return extract_rtf_pages(abs_path, _max_text_bytes(opts))
    if ext in (".htm", ".html"):
        return extract_plain_pages(abs_path, _max_text_bytes(opts), is_html=True)
    # 其余文本类
    return extract_plain_pages(abs_path, _max_text_bytes(opts))


def _max_text_bytes(opts):
    mb = int(opts.get("max_text_mb") or DEFAULT_MAX_TEXT_MB)
    if mb <= 0:
        mb = DEFAULT_MAX_TEXT_MB
    return mb * 1024 * 1024


# ---------------------------------------------------------------------------
# 上限裁剪（输出前统一应用，防输出超长/超内存）
# ---------------------------------------------------------------------------

def _apply_limits(pages, opts):
    """应用每页字符数/每文件页数与字符数上限，返回 (pages, warnings)。"""
    max_pages = int(opts.get("max_pages_per_file") or 0)
    max_chars_page = int(opts.get("max_chars_per_page") or 0)
    max_chars_file = int(opts.get("max_chars_per_file") or 0)
    warnings = []

    # 1) 每页字符数上限
    if max_chars_page > 0:
        for pg in pages:
            if len(pg.content) > max_chars_page:
                pg.content = pg.content[:max_chars_page] + TRUNCATED_MARK
                pg.truncated = True

    # 2) 每文件页数与累计字符数上限
    kept, acc = [], 0
    total = len(pages)
    for i, pg in enumerate(pages):
        if max_pages > 0 and len(kept) >= max_pages:
            warnings.append("达到 max_pages_per_file=%d，其后 %d 页已省略" % (max_pages, total - i))
            break
        if max_chars_file > 0 and acc + len(pg.content) > max_chars_file:
            room = max_chars_file - acc
            if room > 0:
                pg.content = pg.content[:room] + TRUNCATED_MARK
                pg.truncated = True
                kept.append(pg)
            warnings.append("达到 max_chars_per_file=%d，其后 %d 页已省略" % (max_chars_file, total - i))
            break
        acc += len(pg.content)
        kept.append(pg)
    return kept, warnings


# ---------------------------------------------------------------------------
# 文件发现
# ---------------------------------------------------------------------------

def _is_junk(name):
    """是否为隐藏/临时/系统文件。"""
    if name.startswith(".") or name.startswith("~") or name.startswith("._"):
        return True
    if name.startswith("~$") or name.endswith(".tmp"):
        return True
    for junk in _JUNK_NAMES:
        if name.startswith(junk):
            return True
    return False


def discover_files(root, opts):
    """返回 (files, skipped)，files 为 [(abs, rel), ...] 已排序。"""
    files, skipped = [], []
    ext_allow = [e.lower() for e in (opts.get("extensions") or [])]
    recursive = bool(opts.get("recursive", True))
    include_hidden = bool(opts.get("include_hidden", False))
    root_path = Path(root)

    for base, dirs, names in os.walk(root):
        if not include_hidden:
            dirs[:] = [d for d in dirs if not d.startswith(".")]
        dirs[:] = sorted(dirs)
        for name in sorted(names):
            if _is_junk(name):
                continue
            if name.startswith(".") and not include_hidden:
                continue
            ext = os.path.splitext(name)[1].lower()
            if ext not in EXT_TYPE:
                skipped.append(os.path.relpath(os.path.join(base, name), root_path))
                continue
            if ext_allow and ext not in ext_allow:
                skipped.append(os.path.relpath(os.path.join(base, name), root_path))
                continue
            full = os.path.join(base, name)
            if os.path.isfile(full):
                rel = os.path.relpath(full, root_path)
                files.append((full, rel))
        if not recursive:
            break

    files.sort(key=lambda item: item[1].lower())
    return files, skipped


# ---------------------------------------------------------------------------
# 单文件处理
# ---------------------------------------------------------------------------

def process_one_file(abs_path, rel_path, opts):
    """抽取单个文件，返回 (doc_dict, error_text|None)。"""
    ext = os.path.splitext(abs_path)[1].lower()
    try:
        stat = os.stat(abs_path)
        raw_pages = extract_pages_by_ext(abs_path, ext, opts)
    except Exception as exc:
        return None, "%s" % exc

    pages, warnings = _apply_limits(raw_pages, opts)
    total_chars = sum(len(p.content) for p in pages)
    empty_pages = [p.number for p in pages if not p.content.strip()]
    meta_warnings = list(warnings)
    if pages and len(empty_pages) == len(pages) and EXT_TYPE.get(ext) != "image":
        meta_warnings.append("所有页均无可提取文本，疑为扫描件，可能需要 OCR")

    metadata = {
        "pages": len(pages),
        "chars": total_chars,
        "format": ext.lstrip("."),
        "type": EXT_TYPE.get(ext, "text"),
        "size_bytes": stat.st_size,
        "mtime": datetime.fromtimestamp(stat.st_mtime).isoformat(timespec="seconds"),
        "truncated": any(p.truncated for p in pages),
        "warnings": meta_warnings,
        "extractor": _extractor_name(ext),
    }

    if opts.get("context_style") == "text":
        # 整段拼接：便于直接投喂 LLM，页边界用分隔行标注
        context = "\n\n".join(
            "========== 第 %d 页 ==========\n%s" % (p.number, p.content) for p in pages
        )
    else:
        context = [p.to_dict() for p in pages]

    doc = {
        "file": rel_path,
        "name": os.path.basename(abs_path),
        "type": EXT_TYPE.get(ext, "text"),
        "context": context,
        "metadata": metadata,
    }
    return doc, None


def _extractor_name(ext):
    kind = EXT_TYPE.get(ext)
    if kind == "pdf":
        return "pdf:%s" % (_import_pdf_backend() or "none")
    if kind == "image":
        return "image"
    if kind == "word":
        return "docx:xml" if ext == ".docx" else "doc:textutil"
    if kind == "excel":
        if ext == ".xls":
            return "xls:xlrd"
        try:
            import_module("openpyxl")
            return "xlsx:openpyxl"
        except ImportError:
            return "xlsx:zip"
    if ext == ".rtf":
        return "rtf:textutil"
    return "text"


# ---------------------------------------------------------------------------
# 顶层流程
# ---------------------------------------------------------------------------

def _resolve_workspace_path(p):
    """相对路径兜底解析：原样不存在时,依次尝试 <bin>/workspace/<rel> → <cwd>/workspace/<rel>。
    兼容多种惯用写法：
      - 已是绝对路径但仅大小写/别名差异 → 原样兜底；
      - 相对工作区根：'10罗勇兴'、'欠息情况/xx.pdf' → <workspaceRoot>/<p>；
      - 带前缀的写法 'workspace/10罗勇兴'、'bin/workspace/10罗勇兴' → 剥离前缀后落到 workspace 根，
        避免拼出 <workspaceRoot>/workspace/... 的错误路径。
    仍找不到返回 None,由调用方保持原始"路径不存在"错误。"""
    rel = p
    for prefix in ("bin/workspace/", "./bin/workspace/", "workspace/", "./workspace/"):
        if rel.startswith(prefix):
            rel = rel[len(prefix):]
            break
    candidates = []
    bin_root = _find_bin_root()
    if bin_root is not None:
        candidates.append(bin_root / "workspace")
    candidates.append(Path.cwd() / "workspace")
    for base in candidates:
        cand = base / rel
        if cand.exists():
            return str(cand)
    if os.path.exists(p):  # 原样可访问(如绝对路径)兜底
        return p
    return None


def run_ingest(opts):
    """执行摄入流程，返回顶层 JSON dict。输入路径无效时抛 ValueError。"""
    in_path = os.path.expanduser(str(opts.get("path") or "").strip())
    if not in_path:
        raise ValueError("缺少必填参数 path（文件或目录路径）")
    if not os.path.exists(in_path):
        # 相对路径兜底:原样 → <bin>/workspace/<path> → <cwd>/workspace/<path>
        resolved = _resolve_workspace_path(in_path)
        if resolved is None:
            raise ValueError("路径不存在: %s" % in_path)
        in_path = resolved

    max_files = int(opts.get("max_files") or 0)
    workers = int(opts.get("workers") or 0)

    if os.path.isfile(in_path):
        ext = os.path.splitext(in_path)[1].lower()
        if ext not in EXT_TYPE:
            return {
                "tool": TOOL_NAME,
                "version": VERSION,
                "input": {"path": in_path, "is_dir": False},
                "processed_at": iso_now(),
                "documents": [],
                "summary": {
                    "files_found": 1,
                    "files_processed": 0,
                    "files_skipped_unsupported": 1,
                    "files_failed": 0,
                    "total_pages": 0,
                    "total_chars": 0,
                    "errors": [{
                        "file": os.path.basename(in_path),
                        "error": "不支持的文件类型 %s（支持: %s）"
                                 % (ext or "(无扩展名)", ", ".join(sorted(EXT_TYPE))),
                    }],
                },
            }
        files = [(in_path, os.path.basename(in_path))]
        skipped = []
    else:
        files, skipped = discover_files(in_path, opts)
    if max_files > 0:
        files = files[:max_files]

    def handle(item):
        abs_path, rel = item
        return process_one_file(abs_path, rel, opts)

    if workers and workers > 0 and len(files) > 1:
        with ThreadPoolExecutor(max_workers=workers) as pool:
            results = list(pool.map(handle, files))   # 保持输入顺序
    else:
        results = [handle(f) for f in files]

    documents, errors = [], []
    for (abs_path, rel), (doc, err) in zip(files, results):
        if err is not None:
            errors.append({"file": rel, "error": err})
        elif doc is not None:
            documents.append(doc)

    total_pages = sum(d["metadata"]["pages"] for d in documents)
    total_chars = sum(d["metadata"]["chars"] for d in documents)

    # 缺文本(疑为扫描件/图片)清单:供"两遍式 OCR"策略使用——
    # 先不带 OCR 快速 ingest 整目录,依据 files_need_ocr 得知哪些文件需单独 ocr:true 补读,
    # 避免对整目录重复 ingest/整体 OCR(省轮次、省上下文、省时间)。
    files_need_ocr = []
    for d in documents:
        md = d.get("metadata") or {}
        if md.get("chars", 0) == 0 and md.get("type") in ("pdf", "image"):
            files_need_ocr.append({
                "file": d.get("file") or d.get("name"),
                "pages": md.get("pages"),
                "reason": "无可提取文本,疑为扫描件/图片,需对该文件 ocr:true 补读",
            })

    return {
        "tool": TOOL_NAME,
        "version": VERSION,
        "input": {
            "path": in_path,
            "is_dir": os.path.isdir(in_path),
            "recursive": bool(opts.get("recursive", True)),
        },
        "processed_at": iso_now(),
        "documents": documents,
        "summary": {
            "files_found": len(files) + len(skipped) if os.path.isdir(in_path) else len(files),
            "files_processed": len(documents),
            "files_skipped_unsupported": len(skipped) if os.path.isdir(in_path) else 0,
            "files_failed": len(errors),
            "total_pages": total_pages,
            "total_chars": total_chars,
            "files_need_ocr": files_need_ocr,
            "errors": errors,
        },
    }


# ---------------------------------------------------------------------------
# 结果落盘：写 bin/temp/parsed_context_YYYY_MM_DD.json
# ---------------------------------------------------------------------------

def _find_bin_root():
    """从脚本位置向上找第一个名为 bin 的目录（agent 的 bin 根目录）。
    找不到时回退到脚本所在目录，保证从任意 cwd 调用都能定位。"""
    script = Path(__file__).resolve()
    for parent in script.parents:
        if parent.name == "bin":
            return parent
    return script.parent


def _default_result_dir():
    """默认结果目录：<bin>/temp。"""
    return _find_bin_root() / RESULT_DIR_NAME


def _resolve_result_target(result_dir, base_name, now, overwrite):
    """确定最终写入路径：同名已存在且不允许覆盖时，追加 _HHMMSS 避免覆盖。"""
    target = result_dir / base_name
    if target.exists() and not overwrite:
        stem, dot, ext = base_name.rpartition(".")
        alt = "%s%s.%s" % (stem, now.strftime(RESULT_ALT_SUFFIX_FMT), ext if dot else "json")
        target = result_dir / alt
    return target


def save_result_file(payload, opts):
    """把结果 JSON 写入 bin/temp/parsed_context_YYYY_MM_DD.json，返回保存信息 dict。

    - save_result=false 时不落盘，返回 None；
    - 落盘失败不阻断主流程，返回 {"error": ...} 便于调用方感知；
    - 结果 JSON 以 ensure_ascii=False 写入，文件用 indent=2 便于人工/工具阅读。
    """
    if not bool(opts.get("save_result", True)):
        return None
    try:
        dir_arg = str(opts.get("result_dir") or "").strip()
        result_dir = Path(dir_arg).expanduser() if dir_arg else _default_result_dir()
        result_dir.mkdir(parents=True, exist_ok=True)
        now = datetime.now()
        base_name = str(opts.get("result_name") or "").strip() or now.strftime(RESULT_NAME_FMT)
        if not base_name.lower().endswith(".json"):
            base_name += ".json"
        overwrite = bool(opts.get("overwrite_result", False))
        target = _resolve_result_target(result_dir, base_name, now, overwrite)
        text = json.dumps(payload, ensure_ascii=False, indent=2)
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(text)
        return {
            "dir": str(result_dir),
            "name": target.name,
            "path": str(target),
            "bytes": len(text.encode("utf-8")),
            "saved_at": iso_now(),
        }
    except Exception as exc:  # 落盘失败不致命：stdout 仍返回完整结果
        return {"error": "结果落盘失败: %s: %s" % (type(exc).__name__, exc)}


# ---------------------------------------------------------------------------
# 参数解析与入口
# ---------------------------------------------------------------------------

# stdin JSON 中允许的路径字段别名
_PATH_KEYS = ("path", "input", "input_path", "dir", "directory", "file")
_ALLOWED_OPTS = {
    "path", "recursive", "include_hidden", "extensions", "max_files",
    "max_pages_per_file", "max_chars_per_page", "max_chars_per_file",
    "max_text_mb", "word_paragraphs_per_page", "context_style", "workers",
    "ocr", "ocr_langs", "ocr_dpi",
    "save_result", "result_dir", "result_name", "overwrite_result", "emit_stdout",
}


def _clean_opts(raw):
    opts = {}
    for key, value in raw.items():
        if key in _ALLOWED_OPTS:
            opts[key] = value
    # 解析路径字段别名
    for key in _PATH_KEYS:
        if key in raw and raw.get(key):
            opts["path"] = raw[key]
            break
    return opts


def load_args():
    """合并 stdin JSON 参数与命令行位置路径（命令行路径优先）。"""
    args = {}
    # CClaw 脚本执行器约定：参数 JSON 从 stdin 传入
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
            pass  # stdin 非 JSON 时忽略，继续走命令行
    # 命令行：python3 ingest_router.py <路径> [<更多参数 JSON>]
    positionals = [a for a in sys.argv[1:] if not a.startswith("-")]
    if positionals:
        args["path"] = positionals[0]
        if len(positionals) > 1:
            try:
                extra = json.loads(positionals[1])
                if isinstance(extra, dict):
                    args.update(extra)
            except (ValueError, json.JSONDecodeError):
                pass
    return _clean_opts(args)


def emit_result(payload, ok=True, code=0):
    print(json.dumps(payload, ensure_ascii=False))
    sys.exit(code)


def main():
    opts = load_args()
    if opts.get("path") is None:
        emit_result({
            "error": "缺少输入路径。用法: python3 ingest_router.py <文件或目录>  "
                     "或 echo '{\"path\":\"/dir\"}' | python3 ingest_router.py",
            "hint": "支持 pdf/jpg/png/docx/doc/xlsx/xlsm/xls/rtf/html 及 txt/csv/md/log 等文本",
        }, ok=False, code=2)
    try:
        payload = run_ingest(opts)
    except ValueError as exc:
        emit_result({"error": str(exc)}, ok=False, code=2)
    except Exception as exc:  # 兜底：stdout 输出错误 JSON，退出码非 0
        emit_result({"error": "%s: %s" % (type(exc).__name__, exc)}, ok=False, code=1)

    # 结果落盘：bin/temp/parsed_context_YYYY_MM_DD.json，路径回填到 stdout 的 result_file
    result_info = save_result_file(payload, opts)
    if result_info is not None:
        payload = dict(payload)
        payload["result_file"] = result_info

    if not bool(opts.get("emit_stdout", True)):
        # 只要文件、不回传超长 JSON：stdout 输出轻量回执
        saved_ok = result_info is not None and "error" not in result_info
        payload = {
            "ok": saved_ok,
            "summary": payload.get("summary"),
            "result_file": payload.get("result_file"),
        }
    emit_result(payload)


# ---------------------------------------------------------------------------
# 自检（不依赖真实文件）
# ---------------------------------------------------------------------------

def selftest():
    # 扩展名映射（含图片）
    assert EXT_TYPE[".pdf"] == "pdf" and EXT_TYPE[".docx"] == "word"
    assert EXT_TYPE[".xlsx"] == "excel" and EXT_TYPE[".jpg"] == "image"
    assert EXT_TYPE[".png"] == "image" and EXT_TYPE[".txt"] == "text"
    # 列号换算
    assert _col_to_index("A1") == 0
    assert _col_to_index("AB12") == 27
    # 解码
    assert _decode_bytes("你好".encode("utf-8")) == "你好"
    assert _decode_bytes("中文".encode("gbk")) == "中文"
    # HTML 清洗
    html = "<html><head><title>x</title></head><body><p>甲</p><p>乙<br/>丙</p></body></html>"
    out = strip_html_to_text(html)
    assert "甲" in out and "乙" in out and "丙" in out and "title" not in out and "<" not in out
    # 上限裁剪：超出 max_pages_per_file 会省略页面并产生警告
    pages = [Page(1, "a" * 100), Page(2, "b" * 100), Page(3, "c" * 100)]
    kept, warns = _apply_limits(pages, {"max_pages_per_file": 2})
    assert len(kept) == 2 and warns and any("max_pages_per_file" in w for w in warns)
    # 每页字符数上限
    pages = [Page(1, "a" * 100), Page(2, "b" * 100)]
    kept, _ = _apply_limits(pages, {"max_chars_per_page": 50})
    assert kept[0].truncated and kept[0].content.endswith(TRUNCATED_MARK)
    # 每文件累计字符上限
    pages = [Page(1, "a" * 100), Page(2, "b" * 100)]
    kept, warns = _apply_limits(pages, {"max_chars_per_file": 120})
    assert len(kept) == 2 and kept[1].truncated and warns and any("max_chars_per_file" in w for w in warns)
    # docx 分页 token：构造最小 XML 校验切块逻辑
    xml_doc = (
        '<w:document xmlns:w="%s"><w:body>'
        "<w:p><w:r><w:t>第一段</w:t></w:r></w:p>"
        '<w:p><w:r><w:t>第二段</w:t></w:r><w:r><w:br w:type="page"/></w:r><w:r><w:t>第三段</w:t></w:r></w:p>'
        "</w:body></w:document>" % W_NS
    )
    root = ET.fromstring(xml_doc)
    ps = list(_docx_blocks(root.find(wq("body"))))
    assert len(ps) == 2
    chunks = _docx_para_chunks(ps[1][1])
    assert len(chunks) == 2 and "第二段" in chunks[0] and "第三段" in chunks[1]
    # 结果落盘：目录定位 + 落盘/防覆盖/默认文件名（写入临时目录，不留污染）
    assert _find_bin_root().name == "bin"
    assert _default_result_dir().name == RESULT_DIR_NAME
    now = datetime.now()
    base = now.strftime(RESULT_NAME_FMT)
    assert base.startswith("parsed_context_") and base.endswith(".json")
    import tempfile
    with tempfile.TemporaryDirectory(prefix="ingest_tmp_") as td:
        info = save_result_file({"tool": TOOL_NAME, "ok": True}, {
            "result_dir": td, "result_name": base,
        })
        assert info is not None and "error" not in info, info
        assert Path(info["path"]).exists()
        assert json.loads(Path(info["path"]).read_text(encoding="utf-8"))["ok"] is True
        # 同名再次保存且不覆盖 -> 追加时间戳，不覆盖首个文件
        info2 = save_result_file({"tool": TOOL_NAME, "ok": False}, {
            "result_dir": td, "result_name": base,
        })
        assert info2["path"] != info["path"]
        assert json.loads(Path(info["path"]).read_text(encoding="utf-8"))["ok"] is True
        # save_result=false -> 不落盘
        assert save_result_file({"tool": TOOL_NAME}, {"result_dir": td, "save_result": False}) is None
    print("selftest ok (%s v%s)" % (TOOL_NAME, VERSION))


if __name__ == "__main__":
    if any(a in ("--selftest", "-t") for a in sys.argv[1:]):
        selftest()
    elif any(a in ("--help", "-h", "help") for a in sys.argv[1:]):
        print(__doc__)
    else:
        main()

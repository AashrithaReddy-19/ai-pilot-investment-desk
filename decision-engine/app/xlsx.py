"""Minimal, dependency-free XLSX reader for the ingest parsers (A5).

Reads the first worksheet of an Office Open XML workbook into rows of strings, the same shape
the CSV path produces, so every parser rule (units, blanks, bracketed negatives) applies
unchanged. Stdlib only (zipfile + ElementTree). Guards: total uncompressed size and row count
are capped, so a zip bomb or a huge sheet is a ParseError, not an outage.

Cell handling: shared strings, inline strings, formula string results and booleans become text;
numbers are written back without float noise (150000.0 -> "150000"). Dates stay serial numbers:
the parsers never read dates as money.
"""
from __future__ import annotations
import io
import re
import zipfile
import xml.etree.ElementTree as ET

MAX_UNCOMPRESSED_BYTES = 20_000_000
MAX_ROWS = 20_000
_NS = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
       "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
       "pr": "http://schemas.openxmlformats.org/package/2006/relationships"}
_REF = re.compile(r"([A-Z]+)(\d+)")


class XlsxError(ValueError):
    pass


def is_xlsx(raw: bytes) -> bool:
    return raw[:4] == b"PK\x03\x04"


def _col_index(letters: str) -> int:
    n = 0
    for ch in letters:
        n = n * 26 + (ord(ch) - 64)
    return n - 1


def _num_text(v: str) -> str:
    try:
        f = float(v)
    except ValueError:
        return v
    if f.is_integer() and abs(f) < 1e15:
        return str(int(f))
    return repr(f)


def _text(el) -> str:
    """All <t> text under a shared/inline string element (rich text runs concatenated)."""
    return "".join(t.text or "" for t in el.iter(f"{{{_NS['m']}}}t"))


def read_first_sheet(raw: bytes) -> list[list[str]]:
    try:
        zf = zipfile.ZipFile(io.BytesIO(raw))
    except zipfile.BadZipFile as exc:
        raise XlsxError("The file is not a readable XLSX workbook.") from exc
    if sum(i.file_size for i in zf.infolist()) > MAX_UNCOMPRESSED_BYTES:
        raise XlsxError(f"The workbook expands beyond {MAX_UNCOMPRESSED_BYTES:,} bytes; refusing to read it.")
    names = set(zf.namelist())
    try:
        wb = ET.fromstring(zf.read("xl/workbook.xml"))
        sheet = wb.find("m:sheets/m:sheet", _NS)
        if sheet is None:
            raise XlsxError("The workbook has no worksheets.")
        rid = sheet.get(f"{{{_NS['r']}}}id")
        rels = ET.fromstring(zf.read("xl/_rels/workbook.xml.rels"))
        target = next((r.get("Target") for r in rels.findall("pr:Relationship", _NS) if r.get("Id") == rid), None)
        if target is None:
            raise XlsxError("The first worksheet's relationship is missing.")
        path = target.lstrip("/") if target.startswith("/") else "xl/" + target
        shared = []
        if "xl/sharedStrings.xml" in names:
            shared = [_text(si) for si in ET.fromstring(zf.read("xl/sharedStrings.xml")).findall("m:si", _NS)]
        ws = ET.fromstring(zf.read(path))
    except KeyError as exc:
        raise XlsxError(f"The workbook is missing a required part: {exc}.") from exc
    except ET.ParseError as exc:
        raise XlsxError("The workbook XML is malformed.") from exc
    rows: list[list[str]] = []
    for row in ws.iterfind("m:sheetData/m:row", _NS):
        if len(rows) >= MAX_ROWS:
            raise XlsxError(f"The sheet has more than {MAX_ROWS:,} rows.")
        cells: dict[int, str] = {}
        for i, c in enumerate(row.findall("m:c", _NS)):
            m = _REF.match(c.get("r") or "")
            col = _col_index(m.group(1)) if m else i
            t = c.get("t")
            v = c.find("m:v", _NS)
            if t == "s" and v is not None:
                try:
                    val = shared[int(v.text)]
                except (ValueError, IndexError, TypeError):
                    val = ""
            elif t == "inlineStr":
                is_ = c.find("m:is", _NS)
                val = _text(is_) if is_ is not None else ""
            elif t == "b" and v is not None:
                val = "TRUE" if v.text == "1" else "FALSE"
            elif v is not None and v.text is not None:
                val = v.text if t in ("str", "e") else _num_text(v.text)
            else:
                val = ""
            cells[col] = val
        width = max(cells) + 1 if cells else 0
        rows.append([cells.get(j, "") for j in range(width)])
    return [r for r in rows if any(c.strip() for c in r)]


def write_simple(rows: list[list]) -> bytes:
    """Tiny writer for tests and fixtures: one sheet, inline strings and numbers."""
    def cell(j: int, i: int, v) -> str:
        col = ""
        n = j + 1
        while n:
            n, rem = divmod(n - 1, 26)
            col = chr(65 + rem) + col
        ref = f"{col}{i + 1}"
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            return f'<c r="{ref}"><v>{v}</v></c>'
        s = str(v).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
        return f'<c r="{ref}" t="inlineStr"><is><t>{s}</t></is></c>'
    sheet_rows = "".join(f'<row r="{i + 1}">' + "".join(cell(j, i, v) for j, v in enumerate(r)) + "</row>"
                         for i, r in enumerate(rows))
    m = _NS["m"]
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml",
                   '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
                   '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
                   '<Default Extension="xml" ContentType="application/xml"/>'
                   '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
                   '<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
                   '</Types>')
        z.writestr("_rels/.rels", f'<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="{_NS["pr"]}">'
                   '<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>'
                   '</Relationships>')
        z.writestr("xl/workbook.xml", f'<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="{m}" xmlns:r="{_NS["r"]}">'
                   '<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>')
        z.writestr("xl/_rels/workbook.xml.rels", f'<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="{_NS["pr"]}">'
                   '<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>'
                   '</Relationships>')
        z.writestr("xl/worksheets/sheet1.xml", f'<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="{m}"><sheetData>{sheet_rows}</sheetData></worksheet>')
    return buf.getvalue()

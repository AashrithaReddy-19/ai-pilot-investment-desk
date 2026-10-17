"""A5 parsers: XLSX uploads, period normalisation, validation findings and duplicate detection."""
from fastapi.testclient import TestClient

from app import ingest, xlsx
from app.main import app

c = TestClient(app)
XLSX_MIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"


def test_xlsx_balance_sheet_matches_the_csv_path():
    rows = [["Account", "Amount in INR"], ["AI software licences", 1200000], ["Rent", 25000], ["Cloud AI platform", 350000]]
    csv_text = "\n".join(",".join(str(v) for v in r) for r in rows) + "\n"
    a = c.post("/balance-sheet/parse", files={"file": ("bs.xlsx", xlsx.write_simple(rows), XLSX_MIME)}).json()
    b = c.post("/balance-sheet/parse", files={"file": ("bs.csv", csv_text.encode(), "text/csv")}).json()
    assert a["format"] == "xlsx" and b["format"] == "csv"
    assert a["candidates"] == b["candidates"]
    assert [x["amount_inr"] for x in a["candidates"]] == [1200000, 350000]


def test_xlsx_pilot_import_and_numbers_without_float_noise():
    rows = [["id", "task", "setup_cost_inr", "annual_benefit_inr", "annual_running_cost_inr"],
            ["P1", "Invoices", 200000.0, 180000, 60000.5]]
    j = c.post("/pilots/import/parse", files={"file": ("p.xlsx", xlsx.write_simple(rows), XLSX_MIME)}).json()
    assert j["valid_rows"] == 1 and j["pilots"][0]["setup_cost_inr"] == 200000
    assert j["pilots"][0]["annual_running_cost_inr"] == 60000          # rounded to whole rupees like CSV


def test_broken_xlsx_is_a_422():
    r = c.post("/balance-sheet/parse", files={"file": ("x.xlsx", b"PK\x03\x04not really a zip", XLSX_MIME)})
    assert r.status_code == 422 and "XLSX" in r.json()["detail"]


def test_monthly_balance_sheet_is_annualised_and_confirm_reports_both():
    data = b"Account,Monthly spend (INR)\nAI software,10000\nRent,5000\n"
    j = c.post("/balance-sheet/parse", files={"file": ("m.csv", data, "text/csv")}).json()
    assert j["period"]["period"] == "monthly" and j["period"]["annualisation_factor"] == 12
    assert j["candidates"][0]["amount_inr"] == 10000 and j["candidates"][0]["annualised_amount_inr"] == 120000
    k = c.post("/balance-sheet/confirm", files={"file": ("m.csv", data, "text/csv")}, data={"rows": "2"}).json()
    assert k["ai_burn_to_date"] == 10000 and k["ai_burn_annualised_inr"] == 120000


def test_year_end_date_header_is_not_read_as_monthly():
    data = b"Account,Amount 31 Mar 2024 (INR)\nAI software,10000\n"
    j = c.post("/balance-sheet/parse", files={"file": ("y.csv", data, "text/csv")}).json()
    assert j["period"]["period"] == "annual" and j["candidates"][0]["annualised_amount_inr"] == 10000


def test_user_period_choice_wins_and_bad_choice_is_422():
    data = b"Account,Amount\nAI software,1000\n"
    j = c.post("/balance-sheet/parse", files={"file": ("q.csv", data, "text/csv")}, data={"period": "quarterly"}).json()
    assert j["period"]["source"] == "user choice" and j["candidates"][0]["annualised_amount_inr"] == 4000
    assert c.post("/balance-sheet/parse", files={"file": ("q.csv", data, "text/csv")},
                  data={"period": "weekly"}).status_code == 422


def test_pilot_csv_monthly_columns_are_annualised_setup_is_not():
    data = (b"id,task,setup_cost_inr,monthly_savings,monthly_running_cost\n"
            b"P1,Invoices,200000,15000,5000\n")
    j = c.post("/pilots/import/parse", files={"file": ("p.csv", data, "text/csv")}).json()
    p = j["pilots"][0]
    assert (p["setup_cost_inr"], p["annual_benefit_inr"], p["annual_running_cost_inr"]) == (200000, 180000, 60000)
    assert j["periods"]["annual_benefit_inr"]["period"] == "monthly"


def test_findings_list_errors_first_and_flag_possible_duplicates():
    data = (b"id,task,setup_cost_inr,annual_benefit_inr\n"
            b"P1,Invoices,200000,180000\n"
            b"P2,invoices,200000,180000\n"
            b"P3,Broken,abc,1\n")
    j = c.post("/pilots/import/parse", files={"file": ("p.csv", data, "text/csv")}).json()
    f = j["findings"]
    assert f[0]["severity"] == "error" and f[0]["line"] == 4 and f[0]["field"] == "setup_cost_inr"
    dup = next(x for x in f if x["code"] == "POSSIBLE_DUPLICATE")
    assert dup["line"] == 3 and "'P1'" in dup["message"]
    assert [x["severity"] for x in f] == sorted((x["severity"] for x in f), key=["error", "warning", "info"].index)


def test_balance_sheet_findings_carry_the_no_ai_row_flag():
    j = c.post("/balance-sheet/parse", files={"file": ("n.csv", b"Account,Amount\nRent,1\n", "text/csv")}).json()
    assert j["findings"][0]["code"] == "BALANCE_NO_AI_ROW" and j["findings"][0]["severity"] == "warning"


def test_parse_rules_document_formats_and_periods():
    assert ingest.PARSE_RULES["period_factors"] == {"annual": 1, "half_year": 2, "quarterly": 4, "monthly": 12}
    assert "xlsx (first sheet)" in ingest.PARSE_RULES["formats"]

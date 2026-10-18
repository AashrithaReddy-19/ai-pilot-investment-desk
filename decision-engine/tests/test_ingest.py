"""Phase 4 ingest: CSV money parsing, balance-sheet candidates/confirm, proposal import review."""
import pytest
from pathlib import Path
from fastapi.testclient import TestClient
from app import ingest
from app.main import app

c = TestClient(app)

D = Path(__file__).resolve().parent.parent / "data"


def bal(rows: str, unit="auto", col=None, propose=None):
    data = {"unit": unit}
    if col:
        data["amount_column"] = col
    if propose:
        data["propose"] = propose
    return c.post("/balance-sheet/parse", files={"file": ("bs.csv", rows.encode("utf-8"), "text/csv")}, data=data)


# ---- parse_money ----

def test_money_parser_edge_cases():
    f = ingest.parse_money
    assert f("(1,23,456)") == -123456            # bracketed negative, Indian separators
    assert f("(500)") == -500
    assert f("2.5 cr") == 25_000_000 and f("2.5crore") == 25_000_000
    assert f("1.2 L") == 120_000                 # inline unit wins over any column multiplier
    assert f("3", ingest.UNIT_MULTIPLIERS["lakh"]) == 300_000
    assert f("50k") == 50_000 and f("2 thousand") == 2_000
    assert f("₹ 1,00,000") == 100_000
    assert f("n/a") is None and f("") is None and f("-") is None and f("12%") is None
    assert f("junk") is None


def test_unit_detection_and_choice():
    j = bal("Account,Amount (₹ lakh)\nRent,2\n").json()
    assert j["unit"]["unit"] == "lakh" and j["unit"]["multiplier"] == 100000
    j = bal("Account,Amount\nAI tools,3\n", unit="lakh").json()          # user choice overrides
    assert j["unit"]["source"] == "user choice" and j["candidates"][0]["amount_inr"] == 300000
    j = bal("Account,Amount,Year\nAI spend,1500000,2025\n").json()       # plain header → rupees
    assert j["unit"]["unit"] == "rupees"


# ---- balance sheet: candidates, never guessed ----

def test_clean_parse_candidates_and_confirm():
    j = bal("Account,Amount in INR\nAI software licences,1200000\nRent,25000\nCloud AI platform,350000\n").json()
    assert [x["label"] for x in j["candidates"]] == ["AI software licences", "Cloud AI platform"]
    assert j["ai_burn_to_date"] is None and j["flags"] == []             # nothing guessed
    r = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", b"Account,Amount in INR\nAI software licences,1200000\nRent,25000\nCloud AI platform,350000\n", "text/csv")},
               data={"rows": "2,4"})
    j2 = r.json()
    assert j2["ai_burn_to_date"] == 1_550_000 and j2["source"] == "observed"
    assert j2["next_fy"]["next_fy_cost_inr"] == 182_000 and j2["next_fy"]["inflation_flag"] is True   # shipped assumptions


def test_no_ai_row_gives_gap_flag_and_none():
    j = bal("Account,Amount\nRent,25000\nPayroll,9000000\n").json()
    assert j["candidates"] == [] and j["ai_burn_to_date"] is None
    assert [f["code"] for f in j["flags"]] == ["BALANCE_NO_AI_ROW"]


def test_two_ambiguious_rows_are_candidates_confirm_picks():
    rows = "Account,Amount\nAI research tooling,400000\nAI software subscription,600000\n"
    j = bal(rows).json()
    assert len(j["candidates"]) == 2
    r = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "3"})
    j2 = r.json()
    assert j2["ai_burn_to_date"] == 600_000 and j2["rows_confirmed"] == [3]
    assert c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")},
                  data={"rows": "99"}).status_code == 422


def test_bracketed_negative_in_balance_sheet():
    rows = "Account,Amount\nAI software licences,\"(1,00,000)\"\n"
    j = bal(rows).json()
    assert j["candidates"][0]["amount_inr"] == -100000                  # a refund-reversal, kept as-is
    j2 = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "2"}).json()
    assert j2["ai_burn_to_date"] == -100000


def test_crore_unit_and_unknown_row_422():
    rows = "Account,Amount (crore)\nAI spending,2.5\n"
    j = bal(rows).json()
    assert j["unit"]["unit"] == "crore" and j["candidates"][0]["amount_inr"] == 25_000_000
    j2 = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")},
                data={"rows": "", "defaults": ""})
    assert j2.status_code == 422


def test_empty_and_wrong_type_files():
    r = c.post("/balance-sheet/parse", files={"file": ("x.csv", b"", "text/csv")})
    assert r.status_code == 422 and "empty" in r.json()["detail"].lower()
    r = c.post("/balance-sheet/parse", files={"file": ("x.csv", b"%PDF-1.4 \x00\x02binary", "application/pdf")})
    assert r.status_code == 422 and "binary" in r.json()["detail"].lower()
    r = c.post("/balance-sheet/parse", files={"file": ("x.csv", "héllo".encode("latin-1"), "text/csv")})
    assert r.status_code == 422 and "UTF-8" in r.json()["detail"]
    assert ingest.parse_balance_sheet("\ufeffh1,h2\nAI tools,5\n".encode("utf-8")).get("rows") == 1   # BOM ok


def test_amount_column_choice_and_unparsable():
    rows = "Account,This Year (INR),Last Year (INR)\nAI software,1200000,900000\n"
    j = bal(rows).json()
    assert len(j["warnings"]) == 1 and j["amount_column_used"] == "This Year (INR)"
    assert bal(rows, col="Last Year (INR)").json()["amount_column_used"] == "Last Year (INR)"
    j4 = bal("Account,Amount\nAI software,\"twelve lakh\"\nMisc,100\n").json()
    assert j4["candidates"][0]["amount_inr"] is None and any("could not parse" in w for w in j4["warnings"])
    assert c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")},
                  data={"rows": "2"}).json()["ai_burn_to_date"] == 1_200_000
    r6 = c.post("/balance-sheet/confirm",
                files={"file": ("bs.csv", "Account,Amount\nAI software,\"twelve lakh\"\nMisc,100\n".encode(), "text/csv")},
                data={"rows": "2"})
    assert r6.status_code == 200 and [f["code"] for f in r6.json()["flags"]] == ["CONFIRMED_ROW_AMOUNT_MISSING"]
    assert r6.json()["ai_burn_to_date"] == 0   # nothing confirmed with an amount; never invented


# ---- proposal import ----

PILOT_CSV = """id,task,setup_inr,savings,type,adoption,readiness,ramp,delay,overlap
AI1,Claims extraction,200000,180000,cashable,70,high,3,1,claims
AI2,Warn notice,200000,180000,cashable,70,high,,,
AI2,Dupe row,200000,180000,cashable,70,high,,,
AI3,Non cash pilot,,,non-cashable,50,low,,,
AI4,Bad savings,200000,n/a,cashable,70,high,,,
AI5,Bad type,200000,180000,barter,70,high,,,
AI6,Bad adoption,200000,180000,cashable,150,high,,,
AI7,Bad ramp,200000,180000,cashable,70,high,three,,
"""


def test_pilot_import_review():
    r = c.post("/pilots/import/parse", files={"file": ("p.csv", PILOT_CSV.encode(), "text/csv")})
    j = r.json()
    assert j["column_map"]["setup_cost_inr"] == "setup_inr"          # alias mapping visible
    assert j["column_map"]["annual_benefit_inr"] == "savings"
    assert j["rows"] == 8 and set(j["column_map"]) == {"id", "task", "setup_cost_inr", "annual_benefit_inr",
                                                       "benefit_type", "adoption_pct", "readiness", "ramp_months", "delay_months", "overlap_group"}
    recs = {r["line"]: r for r in j["records"]}
    assert recs[2]["pilot"]["setup_cost_inr"] == 200000 and recs[2]["pilot"]["task"] == "Claims extraction"
    assert recs[2]["extension"]["adoption_pct"] == 70 and recs[2]["extension"]["readiness"] == "high"
    assert recs[2]["pilot"]["ramp_months"] == 3 and recs[2]["pilot"]["delay_months"] == 1
    assert "errors" not in recs[3] and recs[3]["extension"] == {"id": "AI2", "adoption_pct": 70, "readiness": "high"}
    assert recs[4]["errors"] == [{"field": "id", "reason": f"duplicate id 'AI2' (already defined in this file)"}]
    assert any(e["field"] == "setup_cost_inr" and "missing" in e["reason"] for e in recs[5]["errors"])
    assert any(e["field"] == "annual_benefit_inr" and "missing" in e["reason"] for e in recs[6]["errors"])
    assert any(e["field"] == "benefit_type" for e in recs[7]["errors"])
    assert any(e["field"] == "adoption_pct" for e in recs[8]["errors"])
    assert any(e["field"] == "ramp_months" for e in recs[9]["errors"])
    assert j["valid_rows"] == 2 and j["error_rows"] == 6
    assert [f["code"] for f in j["flags"]] == ["IMPORT_ROWS_REJECTED"]


def test_pilot_import_confirm_and_fixture_collision_warning():
    files = {"file": ("p.csv", PILOT_CSV.encode(), "text/csv")}
    j = c.post("/pilots/import/confirm", files=files, data={"keep": "AI1,AI2"}).json()
    assert [p["id"] for p in j["pilots"]] == ["AI1", "AI2"]
    assert j["extension_pilots"] == [
        {"id": "AI1", "adoption_pct": 70, "readiness": "high"},
        {"id": "AI2", "adoption_pct": 70, "readiness": "high"}]
    j2 = c.post("/pilots/import/confirm", files=files, data={"keep": "AI2"}).json()
    assert j2["extension_pilots"] == [{"id": "AI2", "adoption_pct": 70, "readiness": "high"}]
    keep = c.post("/pilots/import/confirm", files=files, data={"keep": "AI1"})
    assert keep.status_code == 200
    j3 = c.post("/pilots/import/confirm",
                files={"file": ("p2.csv", b"id,task,setup_cost_inr,annual_benefit_inr\nAI1,Claims,200000,180000\n", "text/csv")},
                data={"keep": "AI1"}).json()
    assert any("shipped fixture" in w for w in j3["warnings"])      # id collides with the shipped fixture
    assert c.post("/pilots/import/confirm", files=files, data={"keep": "NOPE"}).status_code == 422
    assert c.post("/pilots/import/confirm", files={"file": ("p.csv", b"id,task\nX,Y\n", "text/csv")},
                  data={"keep": "X"}).status_code == 422 and "resource" not in keep.text


def test_pilot_import_row_with_money_less_run_allowed():
    j = c.post("/pilots/import/parse",
               files={"file": ("p.csv", b"id,task,setup_cost_inr,annual_benefit_inr\nAI9,Focus,100000,90000\n", "text/csv")}).json()
    assert j["valid_rows"] == 1 and j["pilots"][0].get("annual_running_cost_inr") is None
    assert j["pilots"][0]["setup_cost_inr"] == 100000
    r = c.post("/pilots/import/parse",
               files={"file": ("p.csv", b"task,setup\nOnly task,100\n", "text/csv")})
    assert r.status_code == 422 and "No id column" in r.json()["detail"]


def test_import_over_limit_is_422():
    big = b"id,task,setup_cost_inr,annual_benefit_inr\n" + b"AI1,X,1,2\n" * 999999
    r = c.post("/pilots/import/parse", files={"file": ("big.csv", big, "text/csv")})
    assert r.status_code == 422 and "byte limit" in r.json()["detail"]


# ---- /defaults visibility ----

def test_defaults_expose_parse_rules():
    j = c.get("/defaults").json()
    rules = j["pipeline_rules"]["parse_rules"]
    assert rules["max_upload_bytes"] == ingest.MAX_UPLOAD_BYTES
    assert "ai" in rules["balance_sheet_keywords"] and rules["unit_multipliers"]["crore"] == 10_000_000


# ---- millions / billions (annual reports state "₹ in million") ----

def test_million_and_billion_units():
    f = ingest.parse_money
    assert f("9,663.3 mn") == 9_663_300_000 and f("2 million") == 2_000_000
    assert f("1.5 bn") == 1_500_000_000 and f("3 billions") == 3_000_000_000
    j = bal("Particulars,Year ended 31.03.2026 (INR millions)\nSoftware additions,\"9,663.3\"\nRent,10\n").json()
    assert j["unit"]["unit"] == "millions" and j["unit"]["multiplier"] == 1_000_000
    assert j["candidates"][0]["amount_inr"] == 9_663_300_000
    j = bal("Account,Amount (₹ bn)\nCloud hosting,2\n").json()
    assert j["unit"]["unit"] == "billions" and j["candidates"][0]["amount_inr"] == 2_000_000_000
    j = bal("Account,Amount\nAI tools,4\n", unit="millions").json()
    assert j["unit"]["source"] == "user choice" and j["candidates"][0]["amount_inr"] == 4_000_000


# ---- propose=all: rows already proposed elsewhere (the gateway's AI reader) ----

def test_propose_all_makes_every_described_row_a_candidate():
    rows = "Particulars,Amount as printed\nAdditions during the year (Movement in software),\"9,663.3 mn\"\nOpenAI subscription,\"1,20,000\"\n"
    j = bal(rows).json()                                   # keyword rule: neither label matches a keyword whole
    assert [x["row"] for x in j["candidates"]] == [2]      # "software" inside the context text matches
    j = bal(rows, propose="all").json()
    assert [x["row"] for x in j["candidates"]] == [2, 3]
    assert j["candidates"][1]["matched"] == [] and j["candidates"][0]["amount_inr"] == 9_663_300_000
    r = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")},
               data={"rows": "3", "propose": "all"})
    assert r.status_code == 200 and r.json()["ai_burn_to_date"] == 120_000
    # without propose=all the same row is not a candidate, so confirm refuses it
    assert c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")},
                  data={"rows": "3"}).status_code == 422
    assert bal(rows, propose="everything").status_code == 422


# ---- the plan from the statement, not only from token assumptions ----

def test_confirm_plans_next_fy_from_the_confirmed_rows():
    rows = "Account,Amount in INR\nPlatform licence,\"1,20,00,000\"\nPrivate-cloud hosting,\"60,00,000\"\nRent,10\n"
    r = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "2,3"})
    j = r.json()
    assert j["ai_burn_to_date"] == 18_000_000
    o = j["observed_forecast"]
    assert o["basis"] == "confirmed_rows" and o["ai_run_cost_inr"] == 18_000_000
    assert o["next_fy_cost_inr"] == 19_440_000            # × 1.20 usage growth × 0.90 price drift (shipped)
    assert o["inflation_flag"] is True
    assert j["next_fy"]["ai_run_cost_inr"] == 170_000     # the token estimate is unchanged and separate
    # a different statement gives a different plan
    r2 = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "3"})
    assert r2.json()["observed_forecast"]["next_fy_cost_inr"] == 6_480_000


def test_monthly_rows_are_annualised_and_missing_amounts_give_no_plan():
    rows = "Account,Monthly cost (INR)\nCloud hosting,50000\n"
    o = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "2"}).json()["observed_forecast"]
    assert o["ai_run_cost_inr"] == 600_000 and o["next_fy_cost_inr"] == 648_000
    rows = "Account,Amount\nAI tools,n/a\nRent,100\n"          # only the row without an amount is confirmed
    o = c.post("/balance-sheet/confirm", files={"file": ("bs.csv", rows.encode(), "text/csv")}, data={"rows": "2"}).json()["observed_forecast"]
    assert o["ai_run_cost_inr"] is None and o["next_fy_cost_inr"] is None   # missing is never zero

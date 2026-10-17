"""Phase 1: official inputs are preserved byte-for-byte. Release-blocking."""
import json
from pathlib import Path
from app.io_utils import sha256_file

D = Path(__file__).resolve().parent.parent / "data"
MANIFEST = json.loads((D / "raw" / "MANIFEST.json").read_text())


def test_raw_files_match_manifest():
    for f in MANIFEST["files"]:
        assert sha256_file(D / "raw" / f["path"]) == f["sha256"], f"{f['path']} was modified"


def test_official_fixture_copies_identical():
    raw = (D / "raw" / "official_fixture.json").read_bytes()
    assert raw == (D / "C23" / "fixture.json").read_bytes() == (D / "starting_state.json").read_bytes()


def test_provenance_labels():
    assert {f["path"]: f["source"] for f in MANIFEST["files"]} == {
        "official_fixture.json": "OFFICIAL_FIXTURE", "starting_state.json": "OFFICIAL_FIXTURE", "extension.json": "TEAM_ASSUMPTION"}


def test_official_fixture_rules_present():
    fx = json.loads((D / "raw" / "official_fixture.json").read_text())
    assert fx["case_id"] == "C23" and fx["synthetic"] is True
    assert fx["missing_values"] == "null means unknown, never zero"
    assert [p["id"] for p in fx["records"]["pilots"]] == ["AI1", "AI2", "AI3", "AI4", "AI5"]
    assert fx["records"]["expected_first_pilot_payback_months"] == 20

"""Every engine endpoint has a published contract, and the engine still matches it.

Fails when an output changes without regenerating the contract files:
    ./venv/bin/python scripts/build_contracts.py   (then bump RULES_VERSION and add a CHANGELOG line)
"""
import json

import pytest
from fastapi.testclient import TestClient

from app.main import app
from app.version import CONTRACT_VERSION, RULES_VERSION
from scripts.contract_tools import CONTRACTS, ENGINE, EXAMPLES, call, validate

c = TestClient(app)


def test_contract_version_file_matches_engine():
    assert (CONTRACTS / "VERSION").read_text().strip() == str(CONTRACT_VERSION)
    assert f"rules version {RULES_VERSION}" in (ENGINE / "INDEX.md").read_text()
    assert f"## {CONTRACT_VERSION}." in (CONTRACTS / "CHANGELOG.md").read_text()


def test_every_route_has_a_contract():
    routes = {(m, r.path) for r in app.routes if getattr(r, "methods", None)
              for m in r.methods if m in ("GET", "POST") and r.path not in ("/openapi.json", "/docs", "/redoc",
                                                                         "/docs/oauth2-redirect")}
    covered = {(m, p.replace("AI1", "{pid}")) for m, p, _, _ in EXAMPLES.values()}
    missing = {(m, p) for m, p in routes if (m, p) not in covered}
    assert not missing, f"endpoints without a contract example: {sorted(missing)}"


@pytest.mark.parametrize("name", sorted(EXAMPLES))
def test_engine_matches_contract(name):
    method, path, body, kind = EXAMPLES[name]
    d = ENGINE / name
    assert json.loads((d / "request.json").read_text()) == {"method": method, "path": path, "body": body}
    r = call(c, method, path, body)
    assert r.status_code == 200, r.text
    schema = json.loads((d / "schema.json").read_text())
    if kind == "json":
        errs = validate(r.json(), schema)
        assert not errs, errs[:10]
        assert r.json() == json.loads((d / "response.json").read_text()), \
            f"{name} output drifted from contracts/engine/{name}/response.json; regenerate the contracts"
    else:
        assert r.headers["content-type"].split(";")[0] == schema["contentMediaType"]
        stored = next(d.glob("response.*")).read_bytes().decode()
        assert r.text == stored, f"{name} text drifted; regenerate the contracts"


def test_validator_rejects_removed_keys_and_type_changes():
    schema = json.loads((ENGINE / "views_dashboard" / "schema.json").read_text())
    good = c.post(*EXAMPLES["views_dashboard"][1:2], json=EXAMPLES["views_dashboard"][2]).json()
    assert validate(good, schema) == []
    bad = {**good}
    bad.pop("kpis")
    assert any("missing required key 'kpis'" in e for e in validate(bad, schema))
    bad2 = {**good, "summary": "text"}
    assert any("$.summary: expected" in e for e in validate(bad2, schema))
    assert validate({**good, "new_field": 1}, schema) == []              # additive is fine

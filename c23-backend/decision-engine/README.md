# C23 Python side (`decision-engine/`)

Contains three things:

1. **Internal AI service** — `app/ai_service.py` (FastAPI). Called only by the Go API. Advisory only.
   - `POST /internal/semantic-overlap` — lexical-domain similarity between pilot descriptions (`app/ai/semantic.py`)
   - `POST /internal/explain` — template explanation of a Go evaluation, checked by `app/ai/grounding.py`
   - Run: `uvicorn app.ai_service:app --port 8001` (runtime deps: `requirements-ai.txt`, Docker: `Dockerfile`)
2. **Data pipeline** — immutable `data/raw/` (+ SHA-256 `MANIFEST.json`), `scripts/audit_data.py`, `scripts/preprocess_data.py`,
   strict validation (`app/schemas.py`) and explicit null policy (`app/pipeline.py`). See section below.
3. **Legacy v0 prototype** — `app/main.py`, `app/core.py`, `app/reports.py` (19 original tests, unchanged and passing).
   Superseded by the Go API for business decisions; kept for reference and backward compatibility.

```
python -m venv .venv && . .venv/bin/activate && pip install -r requirements.txt
python -m pytest -q                      # 139 tests
python -m evals.run_evals                # AI evals -> reports/ai_evals.md (exit 1 if a gate fails)
ruff check .
uvicorn app.main:app --reload            # legacy v0 API (prototype)
```

## AI evals (`evals/`)

Ground truth is the Go engine's output (`evals/fixtures/evaluations.json`, written by `go run ./cmd/evidence`).
Suites: semantic overlap precision/recall on team-authored dev + holdout pairs (gates fixed before the first run),
semantic properties (symmetry, determinism, AI4/AI5 detected, fallback to overlap_group), explanation grounding,
hallucination resistance (130 adversarial edits must be rejected, 37 faithful controls accepted), consistency.
All results are labelled SYNTHETIC EVAL — not real-world performance.

## Data pipeline (backend reproduction)

```
python -m scripts.audit_data          # read-only audit  -> reports/data_audit.json, reports/data_audit.md
python -m scripts.preprocess_data     # strict + scenario -> data/processed/pilots_clean*.json/.csv
```
Both accept `--fixture <json|csv> --extension <json> --out-dir <dir>` and exit 2 on unparseable input.
Outputs carry no timestamps: the same inputs produce byte-identical files (tested).

Missing-value policy (null = unknown, never zero): setup/benefit never imputed (REVISE); running cost unknown in STRICT,
`setup × 20%` flagged `RUN_COST_ASSUMED` in SCENARIO; actual adoption/quality `NOT_MEASURED`; unknown overlap group is
`OVERLAP_UNKNOWN`, never "no overlap". Provenance labels: `OFFICIAL_FIXTURE`, `TEAM_ASSUMPTION`, `SYNTHETIC_TRAINING`,
`OBSERVED`, `CALCULATED`, `UNKNOWN`.

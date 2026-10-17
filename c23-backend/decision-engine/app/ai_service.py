"""Internal AI/analytics service (FastAPI). Called ONLY by the Go API; not a public business API.

Run: uvicorn app.ai_service:app --port 8001
Endpoints: GET /health, POST /internal/semantic-overlap, POST /internal/explain
All outputs are advisory. Deterministic finance/decisions live in the Go engine (c23-rules-v1).
"""
from __future__ import annotations
from typing import Optional
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, ConfigDict, Field

from .ai import explain as explainer, semantic

app = FastAPI(title="C23 internal AI service", version="1.0.0", docs_url="/internal/docs", openapi_url="/internal/openapi.json")


class PilotText(BaseModel):
    model_config = ConfigDict(extra="forbid")
    id: str = Field(pattern=r"^[A-Za-z0-9_-]{1,64}$")
    text: str = Field(max_length=2000)
    overlap_group: Optional[str] = Field(default=None, max_length=100)


class OverlapRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    pilots: list[PilotText] = Field(min_length=2, max_length=20)


class FailedRule(BaseModel):
    model_config = ConfigDict(extra="allow")
    code: str
    threshold: Optional[str] = None
    actual: Optional[str] = None


class Evaluation(BaseModel):
    """Subset of the Go evaluation the explainer needs; other fields are kept for grounding."""
    model_config = ConfigDict(extra="allow")
    pilot_id: str = Field(pattern=r"^[A-Za-z0-9_-]{1,64}$")
    decision: str = Field(pattern=r"^(PROCEED|REVISE|STOP|REVIEW)$")
    primary_reason: str
    reason_codes: list[str]
    failed_rules: list[FailedRule] = []
    warnings: list[str] = []
    calculated_metrics: dict = {}


class ExplainRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    evaluation: Evaluation


@app.get("/health")
def health():
    return {"status": "ok", "advisory_only": True,
            "capabilities": {"semantic_overlap": f"{semantic.MODEL_NAME}@{semantic.MODEL_VERSION}",
                             "explain": f"{explainer.MODEL_NAME}@{explainer.MODEL_VERSION}"}}


@app.post("/internal/semantic-overlap")
def semantic_overlap(req: OverlapRequest):
    ids = [p.id for p in req.pilots]
    if len(set(ids)) != len(ids):
        raise HTTPException(422, "duplicate pilot ids")
    pairs = semantic.compare_all([p.model_dump() for p in req.pilots])
    return {"model_name": semantic.MODEL_NAME, "model_version": semantic.MODEL_VERSION, "advisory": True,
            "threshold": semantic.THRESHOLD, "pairs": pairs,
            "note": "Advisory only. The official overlap_group decides benefit accounting and approval."}


@app.post("/internal/explain")
def explain(req: ExplainRequest):
    return explainer.explain_checked(req.evaluation.model_dump())

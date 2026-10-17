"""Advisory semantic-overlap detection between pilot descriptions.

Method "lexical-domain-v1" (deterministic, explainable, no model download):
  similarity = 0.5 * cosine(content tokens) + 0.5 * jaccard(domain concepts)
  possible_overlap = similarity >= THRESHOLD

Why not sentence-transformers: C23 compares 5-8 short task descriptions. A ~400 MB embedding model adds
startup cost, version-dependent non-determinism and an opaque score, for no demonstrated gain on this data.
The official overlap_group stays AUTHORITATIVE; this only suggests pairs a reviewer may have missed.
If a description is empty, the result falls back to the overlap_group rule (method "overlap_group_fallback").
"""
from __future__ import annotations
import math, re
from itertools import combinations
from typing import Optional

MODEL_NAME = "lexical-domain-overlap"
MODEL_VERSION = "1.1.0"
THRESHOLD = 0.30

# Certainty band (advisory). Sharing a business process is not enough to be sure two pilots save the SAME hours:
#   likely   = same process AND same kind of work (e.g. both draft replies)
#   possible = same process but different/unclear work, or lexical similarity only -> human review
#   unlikely = below threshold
ACTIVITIES: dict[str, str] = {}
for activity, terms in {
    "respond": "reply respond response draft answer write compose copilot",
    "classify": "tag classify classification categorize categorise triage route label prioritise prioritize",
    "extract": "extract parse capture read scan ocr",
    "summarise": "summarise summarize summary digest",
    "recommend": "recommend suggest recommendation suggestion knowledge",
    "forecast": "forecast predict prediction estimate",
    "reconcile": "reconcile match matching",
}.items():
    for t in terms.split():
        ACTIVITIES[t] = activity

STOPWORDS = frozenset("""a an and are as at be by for from in into is it its of on or the to with without per via each every
their them they this that these those our your we you all any first new weekly daily monthly""".split())

# Domain lexicon (TEAM_ASSUMPTION): stemmed term -> business process whose hours/revenue it touches.
CONCEPTS: dict[str, str] = {}
for concept, terms in {
    "support": "support ticket customer reply helpdesk complaint agent enquiry inquiry chat callback escalation",
    "finance": "invoice ledger payable receivable reconciliation expense payment accounting bill vendor finance",
    "order_ops": "order exception shipment delivery fulfilment fulfillment logistics warehouse return dispatch",
    "catalog": "catalogue catalog product listing sku attribute merchandising",
    "hr": "candidate resume hiring recruit onboarding payroll employee leave interview",
    "sales_marketing": "lead campaign sales prospect crm marketing promotion",
    "procurement": "purchase procurement supplier quotation tender",
}.items():
    for t in terms.split():
        CONCEPTS[t] = concept


def stem(w: str) -> str:
    """Tiny suffix stripper; applied identically to text and lexicon."""
    if len(w) > 4 and w.endswith("ies"):
        return w[:-3] + "y"
    if len(w) > 4 and re.search(r"(ss|x|ch|sh)es$", w):
        return w[:-2]
    if len(w) > 3 and w.endswith("s") and not w.endswith("ss"):
        w = w[:-1]
    for suf in ("ing", "ed"):
        if w.endswith(suf) and len(w) - len(suf) >= 3:
            return w[: -len(suf)]
    return w


STEMMED_CONCEPTS = {stem(k): c for k, c in CONCEPTS.items()}
STEMMED_ACTIVITIES = {stem(k): a for k, a in ACTIVITIES.items()}


def activities(toks: list[str]) -> set[str]:
    return {STEMMED_ACTIVITIES[t] for t in toks if t in STEMMED_ACTIVITIES}


def tokens(text: str) -> list[str]:
    return [stem(w) for w in re.findall(r"[a-z]+", text.lower()) if w not in STOPWORDS and len(w) > 1]


def concepts(toks: list[str]) -> set[str]:
    return {STEMMED_CONCEPTS[t] for t in toks if t in STEMMED_CONCEPTS}


def _cosine(a: set[str], b: set[str]) -> float:
    if not a or not b:
        return 0.0
    return len(a & b) / math.sqrt(len(a) * len(b))


def compare(a_text: str, b_text: str, a_group: Optional[str] = None, b_group: Optional[str] = None) -> dict:
    """Pure, deterministic and symmetric."""
    if not a_text.strip() or not b_text.strip():
        same = a_group is not None and a_group == b_group
        return {"semantic_similarity": 1.0 if same else 0.0, "possible_overlap": same, "band": "likely" if same else "unlikely",
                "method": "overlap_group_fallback", "reason": "description unavailable; fell back to the official overlap_group rule"}
    ta, tb = tokens(a_text), tokens(b_text)
    sa, sb = set(ta), set(tb)
    ca, cb = concepts(ta), concepts(tb)
    cj = len(ca & cb) / len(ca | cb) if (ca | cb) else 0.0
    sim = round(0.5 * _cosine(sa, sb) + 0.5 * cj, 4)
    shared_c, shared_t = sorted(ca & cb), sorted(sa & sb)
    if shared_c or shared_t:
        reason = "shared concepts: " + (", ".join(shared_c) or "none") + "; shared terms: " + (", ".join(shared_t[:8]) or "none")
    else:
        reason = "no shared domain concept or term"
    shared_a = sorted(activities(ta) & activities(tb))
    possible = sim >= THRESHOLD
    if not possible:
        band = "unlikely"
    elif shared_c and shared_a:
        band = "likely"
        reason += "; same kind of work: " + ", ".join(shared_a)
    else:
        band = "possible"
        reason += "; different or unclear kind of work: needs human review"
    if a_group and a_group == b_group:
        reason += " (official overlap_group also matches)"
    return {"semantic_similarity": sim, "possible_overlap": possible, "band": band, "method": "lexical-domain-v1", "reason": reason}


def compare_all(pilots: list[dict]) -> list[dict]:
    """All unordered pairs, in stable id order."""
    ps = sorted(pilots, key=lambda p: p["id"])
    out = []
    for a, b in combinations(ps, 2):
        r = compare(a["text"], b["text"], a.get("overlap_group"), b.get("overlap_group"))
        out.append({"pilot_a": a["id"], "pilot_b": b["id"], **r})
    return out

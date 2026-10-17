"""OPTIONAL advisory ML experiment on SYNTHETIC data. Never changes PROCEED / REVISE / STOP.

Target: adoption_success = 1 if actual_adoption_pct >= minimum_adoption_pct else 0. Decisions are NOT predicted:
they are produced by deterministic rules, so a model would only learn the rules back.
Features: only information available before the pilot runs (FEATURES). FORBIDDEN columns would leak the outcome.
Results describe performance on synthetic test data only and do not prove real-world predictive performance.
"""
from __future__ import annotations

NUMERIC_FEATURES = ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "expected_adoption_pct", "minimum_adoption_pct",
                    "expected_quality_pct", "minimum_quality_pct", "data_readiness_pct", "technical_readiness_pct", "process_readiness_pct")
CATEGORICAL_FEATURES = ("benefit_type", "department_group", "overlap_group")
FEATURES = NUMERIC_FEATURES + CATEGORICAL_FEATURES
# Outcome columns and anything derived from them or from the rule decision.
FORBIDDEN = frozenset({"actual_adoption_pct", "actual_quality_pct", "decision", "decision_reason", "adoption_success",
                       "breakeven_adoption_pct", "evidence_status", "record_id", "notes", "realized_cash_saving_inr"})
RANDOM_STATE = 42
TEST_SIZE = 0.2
# TEAM bands for presenting the advisory probability (not tuned on outcomes).
RISK_BANDS = ((0.5, "HIGH"), (0.75, "MEDIUM"), (1.01, "LOW"))
# Interesting decision x risk combinations and how to read them (the rule decision always stands).
COMBINATION_NOTES = {
    ("PROCEED", "HIGH"): "Rules permit the pilot, but adoption is at risk: fund it time-boxed and measure adoption early.",
    ("PROCEED", "MEDIUM"): "Rules permit the pilot; plan an adoption check-in before scaling.",
    ("REVISE", "HIGH"): "Consistent signals: revise the plan (adoption support, readiness) before funding.",
    ("STOP", "LOW"): "Adoption looks likely, but a hard rule (quality, economics or break-even) still stops the pilot; ML cannot override it.",
}


def leakage_check(features=FEATURES) -> dict:
    leaked = sorted(set(features) & FORBIDDEN)
    return {"features": list(features), "forbidden": sorted(FORBIDDEN), "leaked": leaked, "status": "PASS" if not leaked else "FAIL"}


def target(p: dict) -> int:
    return int(p["actual_adoption_pct"] >= p["minimum_adoption_pct"])


def risk_band(prob: float) -> str:
    return next(label for upper, label in RISK_BANDS if prob < upper)


def available() -> bool:
    try:
        import sklearn  # noqa: F401
        return True
    except ImportError:
        return False


def run(rows: list, decisions: dict) -> dict:
    """Train/evaluate DummyClassifier, LogisticRegression, RandomForest, GradientBoosting. Deterministic (random_state=42)."""
    lc = leakage_check()
    if lc["status"] != "PASS":
        raise ValueError(f"leakage: {lc['leaked']}")
    if not available():
        return {"status": "SKIPPED", "reason": "scikit-learn not installed (pip install -r requirements-ml.txt)", "leakage_check": lc}
    import numpy as np
    from sklearn.compose import ColumnTransformer
    from sklearn.dummy import DummyClassifier
    from sklearn.ensemble import GradientBoostingClassifier, RandomForestClassifier
    from sklearn.linear_model import LogisticRegression
    from sklearn.metrics import accuracy_score, confusion_matrix, f1_score, precision_score, recall_score, roc_auc_score
    from sklearn.model_selection import StratifiedKFold, cross_val_predict, cross_validate, train_test_split
    from sklearn.pipeline import Pipeline
    from sklearn.preprocessing import OneHotEncoder, StandardScaler

    X = np.array([[p[f] for f in FEATURES] for p in rows], dtype=object)
    y = np.array([target(p) for p in rows])
    ids = [p["record_id"] for p in rows]
    num_idx, cat_idx = list(range(len(NUMERIC_FEATURES))), list(range(len(NUMERIC_FEATURES), len(FEATURES)))
    X_tr, X_te, y_tr, y_te, id_tr, id_te = train_test_split(X, y, ids, test_size=TEST_SIZE, random_state=RANDOM_STATE, stratify=y)

    def pipe(model):
        pre = ColumnTransformer([("num", StandardScaler(), num_idx), ("cat", OneHotEncoder(handle_unknown="ignore"), cat_idx)])
        return Pipeline([("pre", pre), ("model", model)])

    models = {
        "DummyClassifier (baseline)": DummyClassifier(strategy="prior"),
        "LogisticRegression": LogisticRegression(max_iter=2000, random_state=RANDOM_STATE),
        "RandomForestClassifier": RandomForestClassifier(n_estimators=300, random_state=RANDOM_STATE, n_jobs=1),
        "GradientBoostingClassifier": GradientBoostingClassifier(random_state=RANDOM_STATE),
    }
    cv = StratifiedKFold(n_splits=5, shuffle=True, random_state=RANDOM_STATE)
    results = []
    for name, m in models.items():
        p = pipe(m).fit(X_tr, y_tr)
        pred, prob = p.predict(X_te), p.predict_proba(X_te)[:, 1]
        cvr = cross_validate(pipe(m), X, y, cv=cv, scoring=["f1", "f1_macro", "roc_auc"])
        r4 = lambda v: round(float(v), 4)
        results.append({"model": name, "accuracy": r4(accuracy_score(y_te, pred)), "precision": r4(precision_score(y_te, pred, zero_division=0)),
                        "recall": r4(recall_score(y_te, pred, zero_division=0)), "f1": r4(f1_score(y_te, pred, zero_division=0)),
                        "macro_f1": r4(f1_score(y_te, pred, average="macro", zero_division=0)), "roc_auc": r4(roc_auc_score(y_te, prob)),
                        "confusion_matrix": confusion_matrix(y_te, pred, labels=[0, 1]).tolist(),
                        "cv5_f1_mean": r4(cvr["test_f1"].mean()), "cv5_f1_std": r4(cvr["test_f1"].std()),
                        "cv5_macro_f1_mean": r4(cvr["test_f1_macro"].mean()), "cv5_macro_f1_std": r4(cvr["test_f1_macro"].std()),
                        "cv5_roc_auc_mean": r4(cvr["test_roc_auc"].mean()), "cv5_roc_auc_std": r4(cvr["test_roc_auc"].std())})
    # model selection on cross-validated ROC-AUC (the 24-row test split alone is too small to choose on)
    real = [r for r in results if not r["model"].startswith("Dummy")]
    best = max(real, key=lambda r: (r["cv5_roc_auc_mean"], r["cv5_macro_f1_mean"]))
    baseline = results[0]
    beats = best["cv5_roc_auc_mean"] > baseline["cv5_roc_auc_mean"] + 0.05
    # advisory probabilities are OUT-OF-FOLD (each pilot scored by a model that never saw it)
    oof = cross_val_predict(pipe(models[best["model"]]), X, y, cv=cv, method="predict_proba")[:, 1]
    advisory = []
    for p, pr in zip(rows, oof):
        d, band = decisions[p["record_id"]], risk_band(float(pr))
        advisory.append({"record_id": p["record_id"], "deterministic_decision": d, "adoption_success_probability": round(float(pr), 4),
                         "adoption_risk_band": band, "actual_adoption_success": target(p),
                         "combination": f"{d} + {band}", "interpretation": COMBINATION_NOTES.get((d, band), "Advisory only; the rule decision stands.")})
    combos = {f"{d} + {b}": sum(a["deterministic_decision"] == d and a["adoption_risk_band"] == b for a in advisory) for d, b in COMBINATION_NOTES}
    return {"status": "RUN", "target": "adoption_success = actual_adoption_pct >= minimum_adoption_pct", "class_balance": {"success": int(y.sum()), "failure": int(len(y) - y.sum())},
            "split": {"train": len(y_tr), "test": len(y_te), "stratified": True, "random_state": RANDOM_STATE}, "leakage_check": lc,
            "models": results, "best_model": best["model"], "selection_rule": "highest 5-fold CV ROC-AUC (ties: CV macro F1)",
            "best_beats_baseline_by_0_05_auc": beats, "advisory": advisory, "interesting_combinations": combos,
            "model_provenance": "No persisted model exists in the project; this is the same seeded, deterministic experiment re-run "
                                "(same code, data, features and random_state), not a new model design.",
            "disclaimer": "These results describe performance on synthetic test data only and do not prove real-world predictive performance. "
                          "ML is advisory and never overrides PROCEED / REVISE / STOP."}

# C23 decision service (Python / FastAPI)
python3 -m venv .venv && . .venv/bin/activate && pip install -r requirements.txt
python -m pytest -q                      # 19 tests
uvicorn app.main:app --reload            # http://127.0.0.1:8000/docs  (interactive API)
Endpoints: POST /pilots /portfolio /portfolio/check /stoprule/{id} /export/csv /export/evidence /save ; GET /saved/{name}
Every POST accepts optional {fixture, extension, budget, mode} overrides for "change one input" challenges.

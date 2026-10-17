# C23 Backend Benchmark (small, local, honest)

> Not production-scale validation. One Windows laptop, loopback only, 100 requests per workload fired **at the same instant**.
> PostgreSQL: real server (embedded-postgres 16). Redis: **miniredis** (Redis protocol, in-memory) — exercises the cache
> path, not Redis performance. AI: the real FastAPI service (single uvicorn worker) or an unreachable port.

Command: `go run ./cmd/bench -inprocess -n 100 -ai http://127.0.0.1:8001 -out reports/benchmark.json` (real Gin router on a real
TCP listener; raw results in `benchmark.json`). Run on the final code (live AI-status fix included), AI health verified `ok` first.

| Configuration | Workload | n | p50 ms | p95 ms | p99 ms | Errors | ai_status | Cache |
|---|---|---|---|---|---|---|---|---|
| Redis ON, AI online | evaluate | 100 | 192.7 | 211.8 | 231.5 | 0% | ok 100 | HIT 80, MISS 20 |
| Redis ON, AI online | portfolio | 100 | 37.8 | 44.4 | 44.9 | 0% | – | HIT 11, MISS 89 |
| Redis OFF, AI online | evaluate | 100 | 122.1 | 197.2 | 201.7 | 0% | ok 100 | MISS 100 |
| Redis OFF, AI online | portfolio | 100 | 31.1 | 36.5 | 37.0 | 0% | – | MISS 100 |
| Redis ON, AI unavailable | evaluate | 100 | 23.9 | 39.0 | 42.1 | 0% | degraded 100 | MISS 100 |
| Redis ON, AI unavailable | portfolio | 100 | 24.3 | 30.0 | 30.0 | 0% | – | HIT 43, MISS 57 |
| Redis OFF, AI unavailable | evaluate | 100 | 28.8 | 39.1 | 44.8 | 0% | degraded 100 | MISS 100 |
| Redis OFF, AI unavailable | portfolio | 100 | 28.4 | 35.0 | 36.0 | 0% | – | MISS 100 |

## Reading the numbers

- **0% errors in all 8 runs**: correctness and availability never depend on Redis or the AI service.
- **AI unavailable is faster, not broken** (evaluate p50 ≈ 24–29 ms): a refused connection fails fast and the deterministic
  result is returned with `ai_status: degraded`. With AI online the explanation round-trip dominates (≈ 120–190 ms p50).
- **Redis does not reduce latency at this scale.** All 100 requests start together on a cold cache (thundering herd) and the
  engine itself costs microseconds for 5 pilots; cache hits also re-check live AI status. Redis is kept because it avoids repeated
  AI calls and is safe (content-addressed keys, circuit breaker) — not as a performance claim.
- Portfolio reads are dominated by PostgreSQL under 100-way concurrency (pool size 10).

An earlier attempt in this session was discarded because the AI service was not running while labelled "AI online".

## Not measured
Multi-node deployment, real Redis server, sustained load, network latency, TLS, data sizes beyond the C23 fixture.

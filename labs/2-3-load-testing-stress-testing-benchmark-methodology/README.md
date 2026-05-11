# Lab 2-3: Load Testing, Stress Testing, and Benchmark Methodology

A complete, defensible performance experiment for a small Go HTTP
service. You bring up the stack, design the workload model, run the
open-loop baseline, **demonstrate coordinated omission empirically**,
execute the **three-run regression protocol**, run a 4-hour soak, and
write an **architecture review** that converts the result into a
deploy decision.

The infrastructure is provided. You write:

- `perf/workload.json` — workload model (deliverable, must be defended in `docs/review.md`)
- `docs/coordinated-omission/analysis.md` — the side-by-side histogram writeup
- `docs/review.md` — the architecture review (~2,000 words)

## One-line reproducibility

From a clean checkout, the open-loop baseline runs end-to-end with:

```bash
make run-sut && make perf-baseline
```

That brings up the stack, waits for healthchecks, and runs the warmup +
measured phases of `perf/k6/baseline.js`. Results land under
`perf/results/baseline/<RUN_ID>/`.

## Prerequisites

- Docker 24+ and Docker Compose v2
- [k6](https://k6.io/docs/getting-started/installation/) 0.50+ on PATH
- Python 3.10+ (used only by `scripts/analyze-regression.py` and
  `scripts/coordinated-omission-summary.py`; no extra packages needed)
- Roughly 8 GB free RAM and a few GB free disk for Postgres + Prometheus

You do **not** need Go on the host — the SUT and downstream stub are
built inside their containers.

## Port conflicts on your dev box

The defaults are 8080 (sut), 8081 (downstream), 9090 (prometheus),
3000 (grafana), 9100 (node-exporter), 9187 (postgres-exporter).
Postgres is intentionally **not** exposed on the host.

If any of those collide with something else on your machine, copy
`.env.example` to `.env` and override:

```bash
cp .env.example .env
$EDITOR .env
# then make run-sut as usual
```

When you change the SUT port, also pass it through to k6:

```bash
SUT_URL=http://localhost:18080 make perf-baseline
```

## Stack overview

| Service             | Port  | What it does                                                              |
|---------------------|-------|---------------------------------------------------------------------------|
| `sut`               | 8080  | The system under test. Three endpoints: `/fast`, `/medium`, `/slow`.      |
| `downstream`        | 8081  | The dependency `/slow` calls. Has `POST /admin/inject` for the stall switch. |
| `postgres`          | 5432  | Backs `/medium`. Seeded with 50k rows.                                    |
| `postgres-exporter` | 9187  | Postgres metrics for the dependencies row of the dashboard.               |
| `node-exporter`     | 9100  | Host CPU, network, TCP states for the network row.                        |
| `prometheus`        | 9090  | Scrapes everything, evaluates the recording rules in `prometheus/rules/`. |
| `grafana`           | 3000  | Provisioned with two dashboards: `use-red-overview` and `soak-resource-trends`. |

Anonymous viewer is enabled on Grafana (or log in as `admin / admin`).

## Service under test endpoints

| Path      | Behavior                                              | Target p99 (in isolation) |
|-----------|-------------------------------------------------------|---------------------------|
| `/fast`   | Pure-CPU response, no I/O.                            | < 5 ms                    |
| `/medium` | Single SELECT through `pgxpool` against `items`.      | 10–30 ms                  |
| `/slow`   | Calls `downstream:/api/widget`, returns the result.   | 50–80 ms (no stall)       |
| `/healthz`, `/metrics` | Excluded from the RED metrics by middleware. | n/a                |

The slow path is the one that exhibits coordinated omission once the
downstream's stall switch is on.

## Running an experiment

```bash
make perf-baseline        # 60s warmup + 5min measured open-loop
make perf-closed-loop     # closed-loop, used only with the stall switch on
make perf-regression      # baseline x3 (pool=10) then candidate x3 (pool=40)
make perf-soak            # 4-hour soak at 50% target_rps (override with SOAK_RPS)
```

Every run writes:

- `summary-*.json` — k6 trends and counters
- `raw-*.json`     — k6 streaming events (use as needed; mostly for debugging)
- `env.txt`        — environment fingerprint (auto-captured)
- `meta.json`      — start/end ISO timestamps, workload SHA, pool size, etc.
- `k6.log`         — full k6 stdout

Then summarise:

```bash
make analyze-regression   # per-endpoint median, sigma, deltap99, 2-sigma decision
make analyze-omission     # closed-loop p99 vs open-loop p99, asserts >=5x ratio
```

## Coordinated omission demo (homework Step 4)

Toggle the downstream stall switch via the admin endpoint:

```bash
# Stall every 100th request for 8 seconds:
curl -X POST http://localhost:8081/admin/inject \
  -H 'content-type: application/json' \
  -d '{"stall_every_n": 100, "stall_duration_ms": 8000}'

# Closed-loop run with VUs=1, captures the omission-affected histogram:
K6_VUS=1 make perf-closed-loop

# Open-loop run, captures the true tail:
make perf-baseline

# Compare:
make analyze-omission

# Reset:
curl -X POST http://localhost:8081/admin/inject \
  -H 'content-type: application/json' \
  -d '{"stall_every_n": 0}'
```

The exit code from `make analyze-omission` is `0` when the open/closed
p99 ratio is ≥5×, `1` otherwise. If you cannot get to 5×, see the
script's failure hint or the homework's "If you get stuck" section.

## Three-run regression protocol (homework Step 5)

`make perf-regression` automates the full protocol with the pool-size
knob:

1. Restarts `sut` with `SUT_DB_POOL_SIZE=10` and runs the baseline 3×.
2. Restarts `sut` with `SUT_DB_POOL_SIZE=40` and runs the candidate 3×.
3. Moves the candidate runs into `perf/results/candidate/`.
4. Runs the analyzer.

The analyzer prints a per-endpoint table and an overall PASS / INVESTIGATE
decision. Paste the table into Section 5 of `docs/review.md`.

If you want to evaluate a different optimization candidate (gzip, batched
downstream, query rewrite), swap the env-var bouncing in
`scripts/three-run-regression.sh` for whatever your candidate flips and
re-run.

## Soak test (homework Step 6)

```bash
make perf-soak           # 4 hours at workload.target_rps / 2
```

While the soak runs, in another terminal start the log-disk collector:

```bash
bash scripts/soak-collect.sh
```

Then open Grafana at <http://localhost:3000/d/soak-resource-trends> with
the time range set to "Last 4 hours", auto-refresh on (1m). Export the
panels (Share -> Export -> Save to PNG) into
`perf/results/soak/<RUN_ID>/dashboards/`.

## Architecture review (homework Step 7)

`docs/review.md` ships as a 7-section template with TODO markers. Fill
every section using real numbers from your runs, not placeholders. The
`expectedOutcome` checklist in the topic guide and the homework's
acceptance gates tell you what each section must contain.

## File layout

```
labs/2-3-load-testing-stress-testing-benchmark-methodology/
  docker-compose.yml
  Makefile
  README.md                  ← you are here
  .env.example               ← copy to .env if any default port conflicts on your dev box
  .gitignore
  sut/                       ← Go service under test
    Dockerfile
    go.mod
    main.go
    internal/
      middleware/red.go
      handlers/{healthz,fast,medium,slow}.go
      db/pool.go
      downstream/client.go
      runtimemetrics/runtime.go
  downstream/                ← Go downstream stub with /admin/inject stall switch
    Dockerfile
    go.mod
    main.go
  postgres/
    init.sql                 ← seeds items table with 50k rows
  prometheus/
    prometheus.yml
    rules/use_red.yml        ← recording rules for the four-layer dashboard
  grafana/
    provisioning/
      datasources/datasources.yml
      dashboards/dashboards.yml
    dashboards/
      use-red-overview.json
      soak-resource-trends.json
  perf/
    workload.json            ← STARTER - tune and defend in docs/review.md
    workload.schema.json
    env.sh                   ← environment fingerprint capture
    k6/
      baseline.js            ← open-loop, constant-arrival-rate
      closed-loop.js         ← closed-loop, intentionally "wrong" for the demo
      soak.js                ← 4-hour soak
      lib/workload.js        ← shared endpoint picker
    results/
      baseline/              ← `make perf-baseline` lands here
      candidate/             ← `scripts/three-run-regression.sh` moves runs here
      coordinated-omission/  ← `make perf-closed-loop` lands here
      soak/                  ← `make perf-soak` lands here
  scripts/
    run-experiment.sh
    three-run-regression.sh
    analyze-regression.py
    coordinated-omission-summary.py
    soak-collect.sh
  docs/
    review.md                ← DELIVERABLE - the architecture review
    coordinated-omission/
      analysis.md            ← DELIVERABLE - the histogram comparison
  runbooks/
    perf-rollback.md
```

## Submitting

Push to a public fork of `kirill-latish/hlsa2-labs` and submit the
repository URL. Verify on a clean clone that:

- `make run-sut` brings everything healthy without manual steps.
- `make perf-baseline` produces a result file under `perf/results/baseline/`.
- `docs/review.md` and `docs/coordinated-omission/analysis.md` have no
  remaining `TODO` markers.
- `perf/results/` contains the runs the homework rubric requires:
  baseline ×3, candidate ×3, coordinated-omission ×2, soak ×1.

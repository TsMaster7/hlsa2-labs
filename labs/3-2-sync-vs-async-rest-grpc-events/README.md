# Lab 3-2: Synchronous vs Asynchronous Communication (REST, gRPC, Events)

Companion lab for HLSA2 topic 245. You bring up a working stack that
models one logical service exposed two ways (REST/JSON and
gRPC/Protobuf), a synchronous call chain (`gateway -> auth -> pricing
-> inventory`), and an event-driven path (producer -> Redpanda ->
consumer -> Postgres). You then drive four experiments under load:

1. **REST vs gRPC** on identical payloads (step 3).
2. **Synchronous availability tax** under an injected fault (step 4).
3. **Async overload + backpressure** with flow control off vs on (step 5).
4. **Replay + idempotency** of a durable event log (step 6).

You finish by applying one targeted improvement (step 7) and writing the
architecture review (step 8).

The infrastructure is provided. You write:

- one design choice for **`perf/workload.json`** (the protocol payload mix is starter-tuned; defend any change in `docs/review.md`)
- **`docs/review.md`** (~1,500-2,000 words) - the architecture review
- the **screenshots** the review cites, under `docs/img/`

## One-line reproducibility

```bash
make up && make env-fingerprint && make bench-protocols
```

That brings up the full stack, waits for healthchecks, captures the
environment fingerprint, and runs the REST vs gRPC bench. Results land
under `perf/results/`.

## Prerequisites

- Docker 24+ and Docker Compose v2
- [k6](https://k6.io/docs/getting-started/installation/) 0.50+ on PATH
- Python 3.10+ (only for the `analyze-*.py` scripts; no third-party packages required)
- `rpk` is invoked **inside** the Redpanda container, so you do not need it on the host
- ~10 GB free RAM and a few GB free disk for Postgres + Prometheus + Redpanda

You do **not** need Go or `protoc` on the host - every binary is built
inside its container and `proto/lookup.pb.go` is committed.

## Port conflicts on your dev box

The stack binds:

| Service     | Host port |
|-------------|-----------|
| Grafana     | 3000      |
| Prometheus  | 9090      |
| lookup REST | 8080      |
| lookup gRPC | 9000      |
| gateway     | 8090      |
| auth        | 8091      |
| pricing     | 8092      |
| inventory   | 8093      |
| producer    | 8094      |
| consumer    | 8095      |
| Redpanda    | 9092      |
| pg-exporter | 9187      |

Postgres is intentionally **not** exposed on the host. If anything
collides, copy `.env.example` to `.env`, override the conflicting
`LAB_*_PORT`, and re-run `make up`. When you change the REST port, also
pass the new URL to the k6 scripts:

```bash
REST_URL=http://localhost:18080 make bench-protocols
```

## Stack overview

| Service             | Purpose                                                                |
|---------------------|------------------------------------------------------------------------|
| `lookup-svc`        | The same `/lookup` operation served over REST/JSON and gRPC/Protobuf.  |
| `auth`/`pricing`/`inventory` | Three hops of the synchronous chain. Each exposes `POST /admin/inject` for fault injection (latency + error rate). |
| `gateway`           | Drives the chain. `CIRCUIT_BREAKER=on` flips on a tripped-on-failures breaker. `GATEWAY_LOOKUP_TRANSPORT=grpc` swaps the hot-path transport. `GATEWAY_AUDIT_MODE=event` converts the audit side-effect from inline DB write to a Kafka emit. |
| `producer`          | HTTP-trigger event generator (k6 / scripts hit it). `FLOW_CONTROL=on` makes it shed load via 429s when the consumer signals overload. |
| `consumer`          | Drains the `events` topic. `CONSUMER_MODE=naive` does plain INSERT; `CONSUMER_MODE=idempotent` upserts on `event_id`. `FLOW_CONTROL=on` bounds the in-memory queue + drops oldest. DLQ + retry budget land here when `CANDIDATE=dlq-retry-budget`. |
| `redpanda`          | Kafka-compatible single-broker. Topics `events` (6 partitions) and `events.dlq` (3 partitions) are created at startup by `redpanda-init`. |
| `postgres`          | Backs the consumer's `events_audit` table - the downstream that `/replay` and `/seed-events` hit. |
| `prometheus`        | Scrapes everything; recording rules in `prometheus/rules/sync-async.yml` precompute the per-row dashboard panels. |
| `grafana`           | Provisioned with the **Sync/Async Overview** dashboard. Anonymous viewer is on; admin login is `admin / admin`. |

## Running the experiments

```bash
# Step 3 - REST vs gRPC. Three runs per protocol by default.
make bench-protocols RUNS=3                       # default payload=small
make bench-protocols RUNS=3 PAYLOAD=large TARGET_RPS=1000
make analyze-protocols

# Step 4 - synchronous availability tax.
make bench-sync-chain LABEL=healthy
make inject-fault HOP=pricing MODE=latency P99_MS=400 ERROR_RATE=0.02
make bench-sync-chain LABEL=faulted
CIRCUIT_BREAKER=on make bench-sync-chain LABEL=faulted-breaker
make analyze-sync-chain
make clear-fault HOP=pricing

# Step 5 - async overload + backpressure.
make bench-async-overload FLOW_CONTROL=off LABEL=no-bp DURATION=5m
make bench-async-overload FLOW_CONTROL=on  LABEL=with-bp DURATION=5m
make analyze-async

# Step 6 - replay + idempotency.
make seed-events WINDOW=24h
make replay WINDOW=24h CONSUMER_MODE=idempotent
make assert-idempotent CONSUMER_MODE=idempotent
make assert-idempotent CONSUMER_MODE=naive
make analyze-replay

# Step 7 - regression for the targeted improvement.
make regression CANDIDATE=grpc-hot-path RUNS=3
make analyze CANDIDATE=grpc-hot-path
```

Every run writes:

- `summary.json` - condensed metrics the analyzers read
- `meta.json` - kind, label, start/end ISO, candidate, env knobs
- `env.txt` - environment fingerprint (auto-captured)
- `k6.log` (load-driven runs) - full k6 stdout
- `events.csv` (async / replay runs) - per-second queue depth and latency

## Architecture review

`docs/review.template.md` is the seven-section template. Copy it to
`docs/review.md` (`make` does NOT do this for you; you copy it once
before you start writing) and fill every TODO with real numbers from
your runs. The `expectedOutcome` checklist in the topic-guide and the
homework rubric tell you what each section must contain.

Every quantitative claim must cite a file in `perf/results/` or
`docs/img/`. `make check-submission` greps the review for filenames and
fails if any are missing.

## File layout

```
labs/3-2-sync-vs-async-rest-grpc-events/
  Makefile
  README.md                          <- you are here
  docker-compose.yml
  .env.example
  .gitignore
  go.mod
  proto/
    lookup.proto                      service contract
    lookup.pb.go                      hand-written, wire-compatible
    lookup_grpc.pb.go                 hand-written, wire-compatible
  cmd/
    lookup-svc/                       REST + gRPC same operation
    chain-svc/                        auth/pricing/inventory share this binary
    gateway/                          sync chain + breaker + CANDIDATE knobs
    producer/                         HTTP-triggered Kafka producer
    consumer/                         flow control + naive/idempotent modes
  internal/
    metrics/    inject/    payload/    kafka/    breaker/
  perf/
    env.sh                            fingerprint capture
    workload.json
    k6/
      bench-protocols-rest.js
      bench-protocols-grpc.js
      bench-sync-chain.js
      bench-async-overload.js
      lib/workload.js
    results/                          .gitkeep + per-run subdirectories
  prometheus/
    prometheus.yml
    rules/sync-async.yml
  grafana/
    provisioning/{datasources,dashboards}/
    dashboards/sync-async-overview.json
  postgres/init.sql                   events_audit table
  scripts/                            shell wrappers + python analyzers
  runbooks/
    sync-chain-incident.md
    async-backpressure.md
  docs/
    review.template.md                copy to review.md, then write
    img/                              dashboard screenshots referenced from review.md
```

## Submitting

Push to your fork of `kirill-latish/hlsa2-labs` and submit the PR URL.
Before submitting, run on a clean clone:

- `make up` brings everything healthy without manual steps
- `make bench-protocols` produces a result under `perf/results/rest/` and `perf/results/grpc/`
- `make check-submission` passes
- `docs/review.md` is between ~1,500 and ~2,000 words and has no remaining `TODO` markers
- `perf/results/` contains every run the rubric requires (protocol x3 each, sync-chain healthy + faulted + optional breaker, async with-bp + no-bp, replay + assert-idempotent x2, regression baseline x3 + candidate x3)

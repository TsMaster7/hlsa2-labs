# Architecture Review — HLSA2 Lab 3-2

> Copy this file to `docs/review.md` and replace every `TODO`. Target
> length: ~1,500-2,000 words. **Every quantitative claim must cite a
> file** under `perf/results/` or `docs/img/`. Unsupported assertions
> ("gRPC is faster") receive no credit; supported assertions
> ("gRPC cut /lookup p99 from 42ms to 11ms at PAYLOAD=large /
> TARGET_RPS=1000, see `perf/results/grpc/...run2/summary.json`")
> receive full credit.
>
> The grader maps your section headings to the rubric, so keep them.

---

## 1. Environment & method (~200 words)

> What machine produced these numbers, what did you measure with, how
> many runs per condition, and what does "real change vs noise" mean
> here?

- Host CPU / memory / OS: TODO (cite `perf/results/env.txt`)
- Container limits (per-service): TODO (cite `perf/results/env.txt`)
- Tooling versions: docker TODO, k6 TODO, Go TODO
- Runs per condition: TODO (rubric requires >=3 for the protocol bench
  and >=3 per side for the regression)
- Noise threshold rule: `|delta_p99| > 2 * max(sigma_base, sigma_cand)`
  (matches the topic guide and `scripts/analyze-regression.py`)
- workload.json SHA-256: TODO (cite `perf/results/meta.json`)

---

## 2. REST/JSON vs gRPC/Protobuf (~300 words)

> The two regimes the topic guide expects: default payload at the
> default rate, and `PAYLOAD=large TARGET_RPS=1000`.

**Default regime (PAYLOAD=small, TARGET_RPS=200):**

| Metric | REST median | REST sigma | gRPC median | gRPC sigma |
|--------|------------:|-----------:|------------:|-----------:|
| p50    | TODO ms     | TODO ms    | TODO ms     | TODO ms    |
| p95    | TODO ms     | TODO ms    | TODO ms     | TODO ms    |
| p99    | TODO ms     | TODO ms    | TODO ms     | TODO ms    |
| p99.9  | TODO ms     | TODO ms    | TODO ms     | TODO ms    |

Verdict: TODO (cite `perf/results/rest/...summary.json` and
`perf/results/grpc/...summary.json`; paste the
`scripts/analyze-protocols.py` verdict line).

**Chatty regime (PAYLOAD=large, TARGET_RPS=1000):**

| Metric | REST median | REST sigma | gRPC median | gRPC sigma |
|--------|------------:|-----------:|------------:|-----------:|
| p99    | TODO ms     | TODO ms    | TODO ms     | TODO ms    |

Why the gap shifts between the two regimes (refer to JSON parsing cost,
HTTP/1.1 head-of-line blocking, HTTP/2 multiplexing, Protobuf encoding
size): TODO

What did NOT change between the two protocols, and what risks moving
to gRPC introduces (operability, reach, debug-by-curl, polyglot
clients): TODO

---

## 3. Synchronous availability tax (step 4, ~300 words)

> Reproduce the multiplication effect empirically.

| Run                          | Success rate | E2E p99   | Pricing p99 |
|------------------------------|-------------:|----------:|------------:|
| healthy                      | TODO %       | TODO ms   | TODO ms     |
| faulted (pricing p99=400ms)  | TODO %       | TODO ms   | TODO ms     |
| faulted, CIRCUIT_BREAKER=on  | TODO %       | TODO ms   | TODO ms     |

Cite: `perf/results/sync-chain/...healthy.../summary.json`,
`perf/results/sync-chain/...faulted.../summary.json`,
`perf/results/sync-chain/...faulted-breaker.../summary.json`.

- Product of per-hop success rates (from Prometheus
  `lab32:hop_success_rate` during the faulted run): TODO. Measured E2E:
  TODO. Why the two should match: TODO (multiplication of independent
  per-hop probabilities).
- How the caller's p99 in the faulted run tracks the slow hop's tail:
  TODO.
- What the breaker fixed (faster failures, pool recovery) and what it
  did NOT fix (the requests that genuinely need pricing still fail):
  TODO. This is the topic's "reduces blast radius, does not remove
  temporal coupling" line, in your numbers.

---

## 4. Async overload & backpressure (step 5, ~250 words)

> Compare the no-bp run against the with-bp run.

| Run            | Max queue depth | Event-to-effect p99 | Shed/drop count |
|----------------|-----------------|---------------------|-----------------|
| no-bp          | TODO            | TODO ms             | TODO            |
| with-bp        | TODO            | TODO ms             | TODO            |

Cite: `perf/results/async/...no-bp.../meta.json`,
`perf/results/async/...with-bp.../meta.json`,
`docs/img/async-divergence.png` (a screenshot of the divergence row of
the dashboard during the no-bp run).

- What happened to per-message handler latency while queue depth
  climbed: TODO. Why this is the canonical "backlog, not handler"
  signal.
- The mechanism the with-bp run used (bounded queue + drop-oldest;
  producer 429 from the consumer's shed signal; consumer-side rate
  limit): TODO.
- New risk under FLOW_CONTROL=on: load shedding means some events are
  silently dropped. Where in the system that becomes visible (DLQ?
  retry budget? upstream alarm?): TODO.

---

## 5. Replay & idempotency (step 6, ~250 words)

> The two-pass replay state-hash test.

| Mode        | Pass 1 row count | Pass 1 hash | Pass 2 row count | Pass 2 hash | Identical? |
|-------------|------------------|-------------|------------------|-------------|------------|
| idempotent  | TODO             | TODO        | TODO             | TODO        | TODO       |
| naive       | TODO             | TODO        | TODO             | TODO        | TODO       |

Cite: `perf/results/replay/assert-idempotent-idempotent.json`,
`perf/results/replay/assert-idempotent-naive.json`.

- Why idempotent mode held: dedupe key + upsert (`ON CONFLICT
  (event_id) DO NOTHING`). Explain in terms of the topic's
  at-least-once delivery semantics.
- Why naive mode did not: TODO.
- Downstream impact during the replay: TODO (cite `perf/results/replay/
  .../events.csv` or the Postgres connections panel of the dashboard
  during the replay window). Did Postgres connections hit the pool
  ceiling? Did write latency spike?
- If you used `REPLAY_RATE_LIMIT`, explain the trade-off between replay
  duration and downstream impact: TODO.

---

## 6. Targeted improvement (step 7, ~200 words)

> Pick one. State the threshold. Report the result honestly.

- Candidate chosen: TODO (`grpc-hot-path` | `async-side-effect` |
  `dlq-retry-budget` | your own)
- Why this candidate was justified by the earlier measurements: TODO
- Baseline runs / candidate runs: TODO / TODO
- `scripts/analyze-regression.py` output (paste verbatim):

```
TODO: paste the analyze-regression.py output from
perf/results/regression/<candidate>/baseline + /candidate
```

- Decision (real change vs within-noise): TODO
- New risk this improvement introduced (gRPC reduces reach; async
  side-effect introduces eventual consistency; DLQ + retry budget
  hides bug-frequency until the DLQ depth alert): TODO

---

## 7. Decision tree + residual production risks (~200 words)

> Use the topic's per-interaction decision tree to justify at least one
> design choice. End with the risks you would put on a production
> runbook.

- Interaction analysed: TODO (e.g. "gateway -> audit write")
- Decision-tree path: TODO ("does the caller need the result inline? ->
  no -> can the consumer be idempotent? -> yes -> event") ->
  recommendation: TODO
- Why the empirical numbers from sections 2-6 support that path: TODO
- Residual production risks that belong on a runbook:
  - TODO (e.g. DLQ depth alert + retry budget exhausted)
  - TODO (e.g. consumer lag SLO + alarm on queue depth above 80% of
    bounded capacity)
  - TODO (e.g. breaker stay-open monitor, so we notice when failures
    are getting shed in production)
- A link to the runbooks: see `runbooks/sync-chain-incident.md` and
  `runbooks/async-backpressure.md` for the proposed on-call playbook.

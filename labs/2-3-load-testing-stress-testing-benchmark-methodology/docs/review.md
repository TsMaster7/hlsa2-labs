# Architecture Review — HLSA2 Lab 2-3

> Target length: ~2,000 words. Every numerical claim must reference a
> file in `perf/results/`. A reviewer who only reads this document and
> clicks through to one or two referenced files must come away convinced.
>
> Replace every `TODO` block. Do not delete the section headings — the
> grader maps them to the rubric.

---

## 1. Workload model (~200 words)

> Answer: what does the system do, under what request mix, and why is
> this representative of production traffic?

**Endpoint mix (from `perf/workload.json`):**

| Endpoint | Weight (%) | Why this share |
|----------|-----------:|----------------|
| /fast    | TODO       | TODO           |
| /medium  | TODO       | TODO           |
| /slow    | TODO       | TODO           |

**Payload sizes (bytes):** TODO — min/p50/p95/max per endpoint, with
the source distribution in production (e.g. "modeled on prod nginx
access logs week of 2025-W17").

**Think time:** TODO — distribution and parameters, e.g. "lognormal,
mean 1.5s, sigma 0.4". Cite where the distribution came from.

**Arrival pattern:** TODO — `constant`, `poisson`, or `burst-then-idle`.
Justify the choice against expected production traffic.

**workload.json SHA-256:** TODO (paste the value from `meta.json`).

---

## 2. Methodology (~300 words)

> Open-loop or closed-loop? Warm-up window and rationale? How many
> runs? Run-to-run variance? Which of the nine failure modes did you
> guard against, and how?

**Generator model:** open-loop, `constant-arrival-rate` executor in
`perf/k6/baseline.js`. Closed-loop is used only in the omission
demonstration in Section 4 and is documented as such.

**Warm-up window:** TODO seconds. Rationale: TODO — typically the
period required for the JIT-equivalent warm paths (here, pgxpool
expansion to `min_conns` and the Go runtime's GC pacing) to stabilise.
The measured window starts after warm-up and is excluded from the
warm-up window's tag.

**Number of runs:** baseline ×3 and candidate ×3 (see
`perf/results/baseline/` and `perf/results/candidate/`). Coordinated
omission demo: 1 closed-loop + 1 open-loop. Soak: 1 × 4h.

**Run-to-run variance σ (p99 across the three baseline runs):**

| Endpoint | σ (ms) |
|----------|-------:|
| /fast    | TODO   |
| /medium  | TODO   |
| /slow    | TODO   |

**Failure modes guarded against (from theory block 6, list at least 3):**

1. TODO (e.g. "coordinated omission: see Section 4")
2. TODO (e.g. "single-run noise: 3-run protocol in Section 5")
3. TODO (e.g. "warm-up bleed: explicit `phase:warmup` vs `phase:measured` tags in baseline.js")

---

## 3. Bottleneck analysis (~400 words)

> Where did the system saturate first? Cite USE-method evidence — CPU
> run-queue, pool wait time, queue depth, retransmits. Not latency.
> Latency is the symptom; saturation is the cause.

**Headline finding:** at TODO RPS, the bottleneck on the baseline
configuration is TODO (e.g. "pgxpool acquire wait time on /medium").

**Saturation evidence:**

| Signal                                                    | Value at saturation | Source                                                |
|-----------------------------------------------------------|--------------------:|-------------------------------------------------------|
| `sut:pgxpool:acquire_wait:p99`                            | TODO                | `perf/results/baseline/<RUN_ID>/...` (Grafana panel)  |
| `sut:pgxpool:utilization`                                 | TODO                | same                                                   |
| `sut:goroutines`                                          | TODO                | same                                                   |
| `host:cpu_run_queue` (load1)                              | TODO                | same                                                   |
| `host:tcp_retransmit:rate5m`                              | TODO                | same                                                   |
| `sut:downstream_call_duration_seconds:p99`                | TODO                | same                                                   |

**Symptom (latency):** /medium p99 climbed from TODO to TODO at the
saturation point. The latency rise is downstream of the pool wait —
when the pool was raised in Section 5, p99 dropped while every other
USE signal stayed flat.

**What I expected vs. what I observed:** TODO (1–2 paragraphs).

---

## 4. Coordinated omission writeup (~300 words)

> What did you see and how did you guard against it? Include the
> side-by-side histogram comparison.

**Setup:** stall switch on the downstream set to `STALL_EVERY_N=100`,
`STALL_DURATION_MS=TODO`. Both runs hit the same SUT at the same
nominal rate (TODO RPS). Closed-loop run: `K6_VUS=TODO`.

**Side-by-side p99 (from `make analyze-omission` / `perf/results/coordinated-omission/`):**

| Endpoint | Closed-loop p99 (ms) | Open-loop p99 (ms) | Ratio |
|----------|---------------------:|-------------------:|------:|
| /fast    | TODO                 | TODO               | TODO  |
| /medium  | TODO                 | TODO               | TODO  |
| /slow    | TODO                 | TODO               | TODO  |

The /slow ratio is the one that crossed 5×. /fast and /medium did
not — explain why (the stall is on the downstream, so only requests
that reach the downstream are exposed to it).

**What each generator was "seeing" and "missing":** TODO (1
paragraph). Reference the analysis in
`docs/coordinated-omission/analysis.md`.

**How I guarded against it in the rest of the homework:** TODO (the
baseline.js scenario uses `constant-arrival-rate`, not `constant-vus`,
so requests keep arriving even while in-flight latency climbs; see
`perf/k6/baseline.js`).

---

## 5. Optimization result (~300 words)

> What did you change, why did you expect it to help, what did the
> three-run protocol show, was the improvement greater than 2σ? If
> not, what did you learn?

**Change under test:** TODO (e.g. "Raised `SUT_DB_POOL_SIZE` from 10
to 40 via `docker compose run -e SUT_DB_POOL_SIZE=40 sut`").

**Hypothesis:** TODO. (E.g. "At 100 RPS the medium endpoint averages
~25 ms of DB-bound work; with a pool of 10 the steady-state
arrival-rate × service-time product (Little's Law) implies a queue
forms above ~400 RPS-equivalent on /medium specifically. Raising the
pool to 40 should eliminate the wait without adding any other work.")

**Result table (paste from `make analyze-regression`):**

```text
TODO: paste the per-endpoint table here, including:
  - baseline median p50/p95/p99/p99.9 + sigma
  - candidate median p50/p95/p99/p99.9 + sigma
  - delta_p99
  - 2 * max(sigma)
  - decision (IMPROVED / noise / REGRESSED)
```

**Decision:** TODO (one of: IMPROVED >2σ, noise within 2σ, REGRESSED).

**Interpretation:** TODO (1 paragraph). If the result was "noise",
discuss honestly — many real optimisations fail this test.

---

## 6. Soak test findings (~200 words)

> Any leaks? If so, what is the projected exhaustion timeline at
> production load?

**Run:** `perf/results/soak/<RUN_ID>/`, duration 4h, RPS TODO (50% of
saturation).

**Five resource trends:**

| Metric                              | Trend                       | Source / panel                                                   |
|-------------------------------------|-----------------------------|------------------------------------------------------------------|
| SUT resident memory                 | TODO ("flat" / "monotonic +X MB/h") | Grafana panel "SUT resident memory"                       |
| SUT open file descriptors           | TODO                        | Grafana panel "SUT open file descriptors"                        |
| TCP connection states (ESTABLISHED + TIME_WAIT) | TODO            | Grafana panel "Host TCP connection states"                       |
| Goroutine count                     | TODO                        | Grafana panel "Goroutine count"                                  |
| Container log disk usage            | TODO                        | Grafana panel "Container log disk usage" (textfile collector)    |

**Conclusion:** TODO. If "no leak observed", say so explicitly — that
is a valid finding. If a leak is observed, project the exhaustion
window at production load and propose a fix.

---

## 7. Deploy decision (~300 words)

> Ship, do not ship, or ship with conditions? What is your rollback
> trigger? What risk do you watch first after deploy?

**Decision:** TODO (one of: SHIP, SHIP WITH CONDITIONS, DO NOT SHIP).

**Justification (1 paragraph):** TODO. Tie this back to the saturation
evidence in Section 3 and the regression result in Section 5. Avoid
adjectives ("looks good", "should be fine"). Quote numbers.

**Rollback trigger (exact metric and threshold):**

> TODO — for example: "Auto-revert the candidate revision if
> `histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{service=\"sut\",endpoint=\"/medium\"}[5m])))`
> exceeds 200 ms for 5 consecutive minutes after rollout."

This is the single most important sentence in the document. A vague
trigger is worse than no trigger.

See `runbooks/perf-rollback.md` for the operator runbook tied to this
threshold.

**First risk to watch after deploy (one paragraph):** TODO.

---

## Appendix A — Environment fingerprint

Paste the contents of `perf/results/baseline/<latest>/env.txt` here.
Or include it as a fenced code block. The exact host you ran on
matters; do not hand-summarise.

```
TODO
```

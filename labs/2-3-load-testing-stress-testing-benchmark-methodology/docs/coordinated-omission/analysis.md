# Coordinated Omission — Side-by-side analysis

This document is the deliverable for homework Step 4.

## Setup

- SUT: `sut` service from this lab, `SUT_DB_POOL_SIZE=TODO`.
- Downstream stall settings during both runs:
  ```
  STALL_EVERY_N    = TODO   (e.g. 100)
  STALL_DURATION   = TODO   (e.g. 8000 ms)
  ```
  Set via `POST http://localhost:8081/admin/inject` before each run;
  reset to `STALL_EVERY_N=0` after.
- Open-loop run: `perf/k6/baseline.js`, `TARGET_RPS=TODO`,
  `MEASURED_S=TODO`.
- Closed-loop run: `perf/k6/closed-loop.js`, `K6_VUS=TODO`,
  `DURATION_S=TODO`.
- Both runs target the *same* `SUT_URL` and the *same* workload mix.

The point is to hold everything constant *except* the way the load
generator chooses when to fire the next request.

## Raw histograms

Both files are produced by `k6 run --summary-export`.

- Closed-loop: `../../perf/results/coordinated-omission/<RUN>/summary-closed-loop.json`
- Open-loop: `../../perf/results/coordinated-omission/<RUN>/summary-open-loop.json`

Run `make analyze-omission` for the per-endpoint table the homework
rubric checks.

## What each histogram is "seeing"

**Closed-loop (`constant-vus`, VUs=TODO).** Each VU fires a request,
waits for the response, then fires the next one. While the downstream
is in its 8 s stall, the VU is blocked; no requests enter the system,
no samples are recorded. The latency histogram contains the requests
that *finished* during the test, but *not* the requests that *would
have arrived* on a real production schedule. The recorded p99 reflects
"how slow are the requests we managed to send", not "how slow is the
service from a user's perspective".

**Open-loop (`constant-arrival-rate`, target=TODO RPS).** The k6
scheduler fires a new request every `1 / TARGET_RPS` seconds
regardless of whether previous requests have completed. During the
8 s stall, ~`8 * TARGET_RPS` requests pile up; their measured latency
includes the time spent queued behind the stall. The histogram now
contains the requests that *would* have been sent in production. The
recorded p99 reflects "how slow is the service when load doesn't pause
to wait for it".

## What each histogram is "missing"

- Closed-loop is missing the **queueing penalty** that production
  traffic pays. It systematically under-reports p99 / p99.9 / max.
- Open-loop is not missing anything inherent to the experiment, but
  it requires `preAllocatedVUs` and `maxVUs` to be high enough that
  the generator does not itself become the bottleneck. Confirm by
  checking the `iterations_dropped` count in the open-loop summary
  is 0 (or very small).

## Numbers (paste from `make analyze-omission`)

```text
TODO: paste the analyzer's output here unmodified, e.g.

  endpoint   closed p99   open p99    ratio (open/closed)
  fast            ...         ...                  ...
  medium          ...         ...                  ...
  slow            ...         ...                  ...

  PASS: peak ratio ...x >= 5x. Coordinated omission successfully demonstrated.
```

## Why /slow is the one that demonstrates the effect

The stall is injected on the downstream. /fast and /medium do not
touch the downstream, so they are unaffected by the stall and their
closed-loop and open-loop p99 numbers should be similar. /slow is the
endpoint whose tail is dominated by the stalled dependency, so it is
the one whose closed-loop p99 is artificially low.

If /slow does not show ≥5× ratio, see the homework's "If you get
stuck" advice and `scripts/coordinated-omission-summary.py` failure
hint: typically the stall duration needs to be longer or the closed-loop
VU count lower.

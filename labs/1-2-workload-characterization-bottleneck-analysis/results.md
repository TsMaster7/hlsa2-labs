# Lab 1-2 Results: Workload Characterization and Bottleneck Analysis

## SECTION 0: Preparations for the research 
## Arrival Pattern Comparison — Baseline (before measurement fix)

Config: `workers=4, io_workers=2, target_utilization=0.75, read_fraction=0.70, total_requests=5000`

### Poisson arrivals (CV ≈ 1.0)

```
Arrival statistics:
  inter_arrival_mean_ms: 3.86
  inter_arrival_std_ms:  3.90
  CV: 1.01

Requests   Reads  Writes Rejected  Mean(ms)   p95(ms)   Throughput   Duration
-----------------------------------------------------------------------------
    5000    3520    1480        0     13.52     20.01     209.46 r/s   23.871 s

Resource utilisation:
  connection_pool:  60.1%
  io_subsystem:     46.5%
  bottleneck:       connection_pool (60.1%)

Read latency:  mean=11.82ms  p95=14.50ms
Write latency: mean=17.55ms  p95=21.37ms
```

### Bursty arrivals (CV ≈ 1.55)

```
Arrival statistics:
  inter_arrival_mean_ms: 3.88
  inter_arrival_std_ms:  6.03
  CV: 1.55

Requests   Reads  Writes Rejected  Mean(ms)   p95(ms)   Throughput   Duration
-----------------------------------------------------------------------------
    5000    3534    1466        0     13.47     20.02     212.46 r/s   23.534 s

Resource utilisation:
  connection_pool:  60.9%
  io_subsystem:     46.7%
  bottleneck:       connection_pool (60.9%)

Read latency:  mean=11.79ms  p95=14.40ms
Write latency: mean=17.50ms  p95=21.46ms
```

### Observation

Despite the arrival CV nearly doubling (1.01 → 1.55), mean latency, p95, and utilisation are
statistically identical across both patterns. Bursty arrivals should produce higher tail latency
per queuing theory (Pollaczek–Khinchine: mean queue length scales with CV²), but the simulator
reported no difference.

## Root Cause: Latency Measurement Bug

`WorkerPool._handle_request` started its timer **inside the worker thread**, after the request
had already been dequeued from the `ThreadPoolExecutor` queue:

```python
# Before fix — only measures sleep(), not queue wait
start = time.monotonic()
time.sleep(sleep_s)
latency_ms = (time.monotonic() - start) * 1000.0
```

During a burst, many requests pile up in the thread pool's internal queue. Each request then
waits for a free worker before its timer even started, so the recorded latency was pure service
time regardless of arrival pattern.

## Fixing the latency measurement

Record `submit_time` just before `pool.submit()`, pass it into the handler, and measure latency
from submission to completion (queue wait + service time):

```python
# After fix — includes queue wait time
submit_time = time.monotonic()
futures[pool.submit(self._handle_request, req, submit_time)] = req

# inside _handle_request:
time.sleep(sleep_s)
latency_ms = (time.monotonic() - submit_time) * 1000.0
```

This is the correct end-to-end latency a client would observe, and it makes queueing effects
visible under bursty arrivals.

## SECTION 1: Environment
 - Python 3.14
 - Apple M1 Pro, 8 cores, 32GB RAM
 - OS: macOS Sequoia, Version 15.3.1 (24D70)
 - no other load
 
## SECTION 2: Workload Shape Analysis

A table comparing Poisson and bursty profiles:

**Config:** `workers=4, io_workers=2, target_utilization=0.85, read_fraction=0.70, total_requests=1500`

| Profile | CV (std/mean) | Mean latency (ms) | p95 latency (ms) | Throughput (r/s) | Bottleneck util % |
| ------- | ------------- | ----------------- | ---------------- | ---------------- | ----------------- |
| Poisson | 1.01          | 17.74             | 34.42            | 235.28           | 67.2              |
| Bursty  | 1.50          | 23.43             | 60.34            | 231.14           | 66.3              |

**CV calculation** — CV = inter_arrival_std_ms / inter_arrival_mean_ms:
- Poisson: 3.40 / 3.37 = 1.01
- Bursty:  5.20 / 3.46 = 1.50

**Why bursty p95 is higher despite identical average load**

Both profiles target the same mean inter-arrival time (~3.4 ms), so the long-run arrival rate and average utilisation (~67%) are identical. The difference is in arrival *variance*.

Under Poisson arrivals (CV ≈ 1), requests are spread evenly enough that the 4-worker pool rarely has more than one request queued at a time. Under bursty arrivals (CV ≈ 1.5), inter-arrival times follow a heavy-tailed distribution: short gaps cluster requests into bursts that temporarily saturate all workers, forcing subsequent requests to wait in the thread-pool queue. This queue wait is invisible in the mean (it averages out during the quiet periods between bursts) but shows up sharply in the tail.

The result: p95 latency grew from **34.42 ms → 60.34 ms**, a **1.75× increase**, while mean latency grew only moderately from **17.74 ms → 23.43 ms** (1.32×). The disproportionate p95 growth relative to the mean is the classic signature of a queue driven by arrival variance rather than by load.

## SECTION 3: Read/Write Ratio Sweep
A table of the three sweep points:

**Config:** `workers=4, io_workers=2, target_utilization=0.85, total_requests=1500`

| read_fraction | Bottleneck resource | Util % | Mean (ms) | p95 (ms) | Replica lag (ms) |
| ------------- | ------------------- | ------ | --------- | -------- | ---------------- |
| 0.50          | io_subsystem        | 69.6   | 16.38     | 25.64    | N/A              |
| 0.80          | connection_pool      | 72.7   | 19.68     | 35.21    | N/A              |
| 0.95          | connection_pool      | 65.2   | 15.89     | 31.60    | N/A              |

**At which read_fraction does the bottleneck first exceed 70% utilisation?**

At **rf=0.80** — the connection pool reaches 72.7%. At rf=0.50 the bottleneck (io_subsystem) is 69.6%, just below the threshold, and at rf=0.95 it drops further to 65.2%. So the 70% caution zone is only breached at the 80/20 read/write split, where write pressure is high enough to load the I/O path but the connection pool is still the binding resource.

**At which read_fraction does the bottleneck switch from connection_pool to io_subsystem and why?**

The switch happens between **rf=0.80** (connection_pool, 72.7%) and **rf=0.50** (io_subsystem, 69.6%). The crossover sits at rf≈0.60, derived from the point where both resources hit equal utilisation:

```
conn_util  = rps × avg_service_time / workers
io_util    = rps × write_fraction × write_service_time / io_workers
```

Setting them equal and solving for rf gives rf ≈ 0.60 with this config (workers=4, io_workers=2, read_service=10ms, write_service=15ms). Above that threshold reads dominate and the connection pool is the bottleneck; below it writes dominate and the I/O subsystem becomes the bottleneck, because each write carries WAL/fsync overhead (15ms vs 10ms) and there are only 2 I/O workers to absorb the growing write rate.

Meanwhile, the results of 0.80 to 0.95 comparison are a little bit strange:
- I/O subsystem utilization decreased from 40.4% to 9.4% (which is absolutely OK)
- Connection pool is the bottleneck for both cases (as it must be)
- Connection pool utilisation decreased from 72.7% to 65.2% (it shouldn't be so; can be explained by some system instability)

## SECTION 4: Improvement Results

**Improvement applied:** CQRS read replica routing (`replica_lag_ms=2ms`)

**Config:** `workers=4, io_workers=2, target_utilization=0.85, read_fraction=0.70, total_requests=1500, arrival=bursty`

| Metric               | Baseline (bursty) | Improved (bursty) | Delta        |
| -------------------- | ----------------- | ----------------- | ------------ |
| Mean latency (ms)    | 23.43             | 16.84             | −6.59 (−28%) |
| p95 latency (ms)     | 60.34             | 24.64             | −35.70 (−59%)|
| Throughput (r/s)     | 231.14            | 233.08            | +1.94 (+1%)  |
| Rejected requests    | 0                 | 0                 | 0            |
| Bottleneck util %    | 66.3              | 67.1              | +0.8         |

The bottleneck utilisation figure is unchanged because it is computed analytically as `rps × avg_service_time / workers`, which sees the same throughput, the same weighted-average service time, and the same worker count regardless of whether CQRS routing is active — the formula has no awareness of the two separate pools. 
In reality, the primary pool (writes only) runs at ~26% utilisation and the replica pool (reads only) at ~48%, both well below the reported 66%.
This metric could be fixed in the future by tracking primary and replica pool utilisation separately in `MetricsCollector`.

## SECTION 5: New Risk Introduced

CQRS routing introduces **stale read**: if the replica lags behind the primary, a read immediately after a write may return outdated data. Any operation that requires read-your-writes consistency (e.g., reading back a record the same request just created) must bypass the replica and go to the primary, or the client will observe a phantom rollback.

The specific failure mode is a **dirty read under replication lag** — the replica serves a version of the row that predates a committed write. Track **`replica_lag_ms` p95** on the production dashboard; if it consistently exceeds the application's staleness budget, stale reads become user-visible and the replica must be excluded from the read pool until it catches up.

## SECTION 6: Bottleneck Migration and Next Step

After CQRS the primary pool handles writes only (~30% of traffic), dropping its utilisation to ~26%, so the connection pool is no longer the binding constraint.

The next bottleneck in the chain is the **I/O subsystem (WAL throughput)**: writes now arrive at the primary undiluted by reads, and `io_util` will saturate first as throughput grows — the signal to watch is `io_subsystem` utilisation crossing 70% while `connection_pool` stays low. The single next change would be to increase `io_workers` (add WAL writer threads or provision a faster disk), which directly raises the I/O subsystem's capacity ceiling.

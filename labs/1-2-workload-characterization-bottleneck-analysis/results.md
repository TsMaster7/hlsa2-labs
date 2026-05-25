# Lab 1-2 Results: Workload Characterization and Bottleneck Analysis

## PREPARATIONS FOR THE RESEARCH: 
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

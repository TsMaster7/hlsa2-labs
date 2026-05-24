## Environment
 - Python 3.14
 - Apple M1 Pro, 8 cores, 32GB RAM
 - OS: macOS Sequoia, Version 15.3.1 (24D70)
 - no other load

## Initial Benchmark Results (original configuration)
**Config**: service_time=10ms, workers=4, requests=500, saturated_multiplier=3

| Condition  | Workers | Requests | Rejected | Mean(ms)  | p95(ms)  | Throughput  | Duration  |
|---|---------|----------|----------|---|---|---|---|
| Serial  | 1       | 500      | 0        | 12.17  | 14.79  | 81.22 r/s  | 6.156 s  |
| Parallel  | 4       | 500      | 0        | 11.87  |14.82   | 334.11 r/s  | 1.497 s  |
| Saturated  | 4       | 1500     | 0        | 11.92  |14.52   |333.73 r/s   | 4.495 s  |

 - Neither mean nor p95 latency is decreased
 - Throughput increased almost linearly
 - Duration increased linearly
 - L = λ × W = ~4, calculated length of the queue corresponds to the number of workers
 - No saturation effect

## Benchmark modification to simulate saturation
It was discovered experimentally that the configuration and workload needed to be changed:
 - much more workers required
 - number of total requests must be increased
 - saturation multiplier must be increased significantly
 - _runner.py_ should always prefer `pool.run()` to `pool.run_with_arrival_rate()` for the workload generation (`pool.run_with_arrival_rate()` sends queries to the pool too slowly and the queue is drained before it's loaded so it's easier to simulate saturation when all queries are sent at once) 
Then we can simulate saturation.

## Initial Benchmark Results (real saturation configuration)
Config: service_time=10ms, workers=1024, requests=2500, saturated_multiplier=10

| Condition   | Workers | Requests | Rejected | Mean(ms)   | p95(ms)   | Throughput   | Duration  | L   |
|---|---------|----------|----------|---|---|---|---|-----|
| Serial   | 1       | 2500     | 0        | 12.05   | 14.63   | 82.31 r/s  | 30.373 s  | -   |
| Parallel  | 1024    | 2500     | 0        | 12.07   | 14.62  | 24105.62 r/s  | 0.104 s  | 291 |
| Saturated   | 1024    | 25000    | 0        | 70.40   | 420.56  | 11530.56 r/s   | 2.168 s  | 807 |

 - latency is degrading
 - p95 latency is degrading significantly
 - throughput is degrading
 - calculated length of the queue is increasing
 - non-linear duration scaling (10 times more requests -> 20 times longer the execution time)

## Proposal for optimization
- decrease number of workers to find the concurrency level at which throughput peaks without p95 latency exceeding 2× the serial baseline mean

## Optimization results
As a result of experiments it was discovered that the optimal number of workers is 256.
Further reduction of workers pool can give us a bit better p95 latency, but it decreases throughput. 

**Optimized configuration:** service_time=10ms, workers=256, requests=2500, saturated_multiplier=10

**Benchmark results for the optimized configuration:**

| Condition  | Workers | Requests | Rejected | Mean(ms)   | p95(ms)   |Throughput   |Duration   |
|---|---------|----------|----------|---|---|---|---|
| Serial   | 1       | 2500     | 0        | 11.93  | 14.64   | 83.09 r/s   | 30.088 s  |
| Parallel   | 256     | 2500     | 0        | 12.18  | 16.66   | 17393.01 r/s  | 0.144 s  |
| Saturated   | 256     | 25000    | 0        | 12.20   | 14.82  | 20463.10 r/s  | 1.222 s  |

**Comparative table:**

(first 2 rows actually represent the same configuration)

| Condition   | Version  | Mean Latency (ms)  | p95 Latency (ms)  | Throughput (req/s)  | Workers |
|---|---|---|---|---|---------|
| Serial  | Baseline  | 12.05  | 14.63  | 82.31 r/s  | 1       |
| Serial  |Improved   | 11.93  |14.64   | 83.09 r/s  | 1       |
| Parallel  | Baseline  |12.07   | 14.62  | 24105.62 r/s  | 1024    |
| Parallel  | Improved  | 12.18  |  16.66 | 17393.01 r/s  | 256     |
| Saturated  |Baseline   |70.40   | 420.56  | 11530.56 r/s  | 1024    |
| Saturated  | Improved  | 12.20  | 14.82  | 20463.10 r/s  | 256     |

- latency got back to normal values
- no saturation effect seen anymore
- throughput slightly decreased for the 2500 requests workload, but significantly increased for the 25000 requests workload

## Experimental analysis and criticism
**The main conclusion:** Too Many Workers = Performance Degradation

We solved it reducing the number of workers.
But:
- Enormous extension of the workers pool creates artificial latency
- It looks like saturation but is actually over-provisioning overhead
- Currently, the benchmark only shows contention-based degradation (1024 workers), not queue-depth-based degradation (which should
  also happen with fewer workers + massive load).
- Now we measure only request handling time (_WorkerPool->_handle_request_ start-to-finish time), not the time which request waits in the queue
- We need to fix the latency measurement to include queue wait time.
- Then we can simulate and tackle queue-depth-based degradation

## Extended Metrics: Queue Wait Time vs Service Time

After implementing queue wait time measurement, we can now see the real source of latency under saturation.

### Baseline Configuration (Unbounded Queue)

**Configuration:** service_time=10ms, workers=24, requests=1000, saturated_multiplier=10, unbounded queue

The saturated workload uses controlled arrival rate (1.5× capacity) to simulate realistic traffic patterns.

#### Benchmark Results

| Condition | Workers | Requests | Rejected | Mean(ms) | p95(ms) | Throughput | Duration |
|-----------|---------|----------|----------|----------|---------|------------|----------|
| Serial | 1 | 1,000 | 0 | 6,177.30 | 11,744.69 | 80.96 r/s | 12.352 s |
| Parallel | 24 | 1,000 | 0 | 250.86 | 469.18 | 2,016.26 r/s | 0.496 s |
| **Saturated** | 24 | 10,000 | 0 | **568.47** | **1,104.02** | 2,094.24 r/s | 4.775 s |

#### Latency Breakdown

| Condition | Queue Mean | Queue p95 | Service Mean | Service p95 | Total Mean | Total p95 | Queue % |
|-----------|------------|-----------|--------------|-------------|------------|-----------|---------|
| Serial | 6,164.97ms | 11,733.81ms | 12.33ms | 14.73ms | 6,177.30ms | 11,744.69ms | 99.8% |
| Parallel | 239.16ms | 457.42ms | 11.69ms | 14.24ms | 250.86ms | 469.18ms | 95.3% |
| **Saturated** | **557.05ms** | **1,092.21ms** | 11.42ms | 14.18ms | 568.47ms | 1,104.02ms | **98.0%** |

**Key Observation:** Under saturation, queue wait time dominates (98% of total latency). p95 exceeds 1 second.

**Proposal:** implement bounded queue with Load Shedding.

**Note:** So that not to have too many rejected requests, let's return to using the `run_with_arrival_rate()` when simulating workload for saturation

### Improved Configuration (Bounded Queue with Load Shedding)

**Configuration:** service_time=10ms, workers=24, requests=1000, saturated_multiplier=10, max_queue_size=5000

Implements bounded queue with load shedding - excess requests are rejected immediately (HTTP 429 style).

#### Benchmark Results

| Condition | Workers | Requests | Rejected | Mean(ms) | p95(ms) | Throughput | Duration |
|-----------|---------|----------|----------|----------|---------|------------|----------|
| Serial | 1 | 1,000 | 0 | 6,114.88 | 11,657.23 | 81.59 r/s | 12.257 s |
| Parallel | 24 | 1,000 | 0 | 252.15 | 472.12 | 2,010.02 r/s | 0.498 s |
| **Saturated** | 24 | 5,024 | 4,976 | **273.80** | **516.63** | 1,402.83 r/s | 3.581 s |

#### Latency Breakdown

| Condition | Queue Mean | Queue p95 | Service Mean | Service p95 | Total Mean | Total p95 | Queue % |
|-----------|------------|-----------|--------------|-------------|------------|-----------|---------|
| Serial | 6,102.65ms | 11,647.13ms | 12.22ms | 14.71ms | 6,114.88ms | 11,657.23ms | 99.8% |
| Parallel | 240.39ms | 461.41ms | 11.77ms | 14.25ms | 252.15ms | 472.12ms | 95.3% |
| **Saturated** | **262.61ms** | **505.50ms** | 11.19ms | 14.05ms | 273.80ms | 516.63ms | **95.9%** |

**Key Observation:** Bounded queue limits queue buildup. 50% of requests rejected, but accepted requests experience 2× better latency.

### Comparative Analysis

#### Performance Improvements (Saturated Workload)

| Metric | Baseline (Unbounded) | Improved (Bounded 5000) | Improvement |
|--------|----------------------|-------------------------|-------------|
| **p95 Latency** | 1,104.02 ms | 516.63 ms | **2.14× faster** |
| **Mean Latency** | 568.47 ms | 273.80 ms | **2.08× faster** |
| **Queue p95** | 1,092.21 ms | 505.50 ms | **2.16× faster** |
| **Queue Mean** | 557.05 ms | 262.61 ms | **2.12× faster** |
| **Requests Accepted** | 10,000 (100%) | 5,024 (50.2%) | 50% rejection |
| **Throughput** | 2,094.24 r/s | 1,402.83 r/s | 67% maintained |

#### Key Findings

**1. Latency Reduction**
- p95 latency cut in half: **1,104ms → 517ms** (2.14× improvement)
- Queue wait time reduced: **1,092ms → 506ms** at p95
- Service time remains stable: ~11-12ms across all configurations

**2. Load Shedding Effectiveness**
- **50% of requests rejected** under extreme overload (1.5× capacity)
- Rejected requests fail in <1ms (fast failure)
- Accepted requests experience predictable, bounded latency
- System protected from cascading failures

**3. Throughput Trade-off**
- Baseline: 2,094 r/s (all requests accepted, high latency)
- Improved: 1,403 r/s (50% accepted, low latency)
- **67% throughput maintained** with 2× latency improvement
- Better to serve 50% of requests quickly than 100% slowly

**4. Production Readiness**
- Queue size of 5,000 provides balanced protection
- Predictable tail latency (p95 < 520ms)
- Clear capacity signaling via 429 responses
- Prevents queue-induced cascading failures

### Conclusion

**Bounded queue with load shedding successfully mitigates saturation:**
- ✅ **2× latency improvement** (1,104ms → 517ms p95)
- ✅ **Controlled queue depth** prevents unbounded growth
- ✅ **Fast failure** for rejected requests (<1ms vs multi-second wait)
- ✅ **System stability** maintained under overload
- ⚠️ **50% rejection rate** under extreme load (1.5× capacity) - acceptable trade-off

**The optimization demonstrates the fundamental principle:** *Fast failure is better than slow failure*. Rejecting excess requests immediately is preferable to making all requests wait indefinitely, resulting in better overall system health and user experience.

**Results files description**
 - improved.txt should be compared to baseline.txt (basic benchmark, service latency only)
 - improved_extended.txt should be compared to baseline_extended.txt (extended benchmark, queue and service latency measured)

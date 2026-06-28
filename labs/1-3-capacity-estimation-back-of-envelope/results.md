## SECTION 1 - Environment
 - Python 3.14
 - Apple M1 Pro, 8 cores, 32GB RAM
 - OS: macOS Sequoia, Version 15.3.1 (24D70)
 - no other load
 - Lab 1-3 initial commit: 8d8cf76
 - Improvement option: D – implementing tiered storage
 - Implementation of the improvemnt: Configure a hot tier sized to 30 days of data and a cold tier on object storage for the rest. Recompute hot-tier bandwidth and storage cost.

## SECTION 2 – Initial capacity estimate

Final results from [capacity-estimate.md](capacity-estimate.md) (one headline value per metric; QPS estimated two independent ways).

| # | Metric | Final estimate |
|---|--------|----------------|
| 1 | Peak QPS — method A (workload formula) | 6,944 req/s |
| 2 | Peak QPS — method B (independent sanity check) | 6,000 req/s |
| 3 | Storage at 12 mo (physical, retained) | 86.26 TB |
| 4 | Peak bandwidth (total egress) | 35.2 MB/s |
| 5 | Working-set RAM per node | ≈ 72 GB |
| 6 | Capacity (nodes / headroom at peak) | 3 nodes / 85.2 % |
| 7 | Cost (monthly total / per 1k QPS) | $9,883 / mo · $1,423 per 1k QPS |


## Section 3 — Estimate vs. measured discrepancies

Measured values from [results/baseline.txt](results/baseline.txt). Ratio = Measured / Estimated.

| Dimension                    | Estimated | Measured | Ratio | Missing multiplier           |
| ---------------------------- | --------: | -------: | ----: | ---------------------------- |
| Peak read QPS                |     6,250 |    6,250 |  1.00 | —                            |
| Peak write QPS               |       694 |   694.44 |  1.00 | —                            |
| Internal write QPS (× RF)    |       521 |  520.8 ¹ |  1.00 | —                            |
| Ingress bandwidth (MB/s)     |      27.1 |    27.13 |  1.00 | —                            |
| Egress bandwidth (MB/s)      |      24.4 |    24.41 |  1.00 | —                            |
| Replication egress (MB/s)    |       5.4 |     5.43 |  1.00 | —                            |
| Storage growth (GB/day)      |    239.62 |   312.42 |  1.30 | write-amp ×1.4 (net ×1.30) ² |
| Working-set memory (GB/node) |        72 |    n/a ³ |     — | not measured                 |
| p99 latency (ms)             |     4.0 ⁴ |     8.75 |  2.19 | queueing tail                |

¹ Not printed directly; derived from the benchmark as avg_write_qps (173.6) × RF (3).

² Write amplification (×1.4), partially offset by decimal→binary GB (÷1.07) ⇒ net ×1.30.

³ The benchmark reports no RAM figure, so there is no measured counterpart to compare.

⁴ Not modeled in capacity-estimate.md; derived here as the cache-miss service time (`per_request_service_time_ms = 4.0`), i.e. the path a tail (p99) request takes.

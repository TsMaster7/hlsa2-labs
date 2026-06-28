## SECTION 1 - Environment
 - Python 3.14
 - Apple M1 Pro, 8 cores, 32GB RAM
 - OS: macOS Sequoia, Version 15.3.1 (24D70)
 - no other load
 - Lab 1-3 initial commit: 8d8cf76
 - Improvement option: D – implementing tiered storage
 - Implementation of the improvement: Configure a hot tier sized to 30 days of data and a cold tier on object storage for the rest. Recompute hot-tier bandwidth and storage cost.
 - Lab 1-3 improvement commit: bc43ba5

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

## Section 4 — Improvement before / after

**Why D:** the system is durability-bound (3 nodes = RF, not throughput) and ~91% of its bill is storage, while 12-month retention keeps ~92% of bytes cold and rarely read — so tiering is the only lever that cuts the real bottleneck (storage cost) without touching durability.

Baseline from [results/baseline.txt](results/baseline.txt), improved from [results/improved.txt](results/improved.txt).

| Metric                    | Baseline | Improved |             Delta |
| ------------------------- | -------: | -------: | ----------------: |
| Peak read QPS observed    |    6,250 |    6,250 |                 0 |
| Peak write QPS observed   |   694.44 |   694.44 |                 0 |
| Storage at 12 months (TB) |   109.84 |   109.84 |               0 ¹ |
| Total bandwidth (MB/s)    |    35.26 |    35.26 |                 0 |
| p99 latency (ms)          |     8.75 |     8.56 |      −0.19 (−2 %) |
| Cost per 1000 QPS ($)     | 1,770.80 |   434.14 | −1,336.66 (−75 %) |

¹ Total bytes are unchanged — D re-tiers the same 109.84 TB (hot 9.12 TB / cold 100.72 TB at 0.1× $/GB); that re-split is what moves the cost line, not any reduction in stored data.

## Section 5 — New risk + monitoring metric

Moving ~92% of data to the cold object tier introduces a **cold-tier latency spike** risk when access drifts off the 30-day hot window (a backfill, an old-data query, or a mis-set partition key), caught by **`cold_tier_read_latency_ms` p99** — paired with **`cold_tier_read_ratio`** (share of reads served from cold) to flag the access-pattern shift before it shows up as user-facing latency.

**Metrics** (both illustrative — derived from app/storage-client instrumentation tagged by `tier`):
- `cold_tier_read_latency_ms` p99 — p99 duration of reads served from the cold tier; from a read-duration histogram: `histogram_quantile(0.99, sum by (le) (rate(storage_read_duration_seconds_bucket{tier="cold"}[5m]))) * 1000`.
- `cold_tier_read_ratio` — fraction of reads hitting cold vs all reads: `rate(storage_reads_total{tier="cold"}[5m]) / rate(storage_reads_total[5m])`.

## Section 6 — Next bottleneck forecast

**Working-set memory (RAM/node)** saturates first. §4 of the estimate puts it at 72 GB = **56% of a 128 GB node**, and it scales ~linearly with load (buffer pool + read cache both grow with volume), so doubling load → ~144 GB = **112% — overflow** — while egress only reaches 56% of a 1 Gbps NIC (70.5 / 125 MB/s) and node-QPS just ~30%. 

**Metric that shows it:** `buffer_pool_hit_ratio` (drops sharply once the hot set spills out of RAM, with `node_memory_used_pct` crossing ~90% behind it). **Single next change:** shard the hot dataset across more nodes so each holds a fraction of the working set (or vertical-scale to a larger-RAM instance — Option C).

**Metrics** (both illustrative — derived from standard exporters):
- `buffer_pool_hit_ratio` — page requests served from the in-RAM buffer pool vs total; falls when the working set no longer fits in RAM. DB-sourced:
  - Postgres: `blks_hit / (blks_hit + blks_read)` (`pg_stat_database`); 
  - InnoDB: `1 - Innodb_buffer_pool_reads / Innodb_buffer_pool_read_requests`.
- `node_memory_used_pct` — OS memory utilisation backstop, from Prometheus `node_exporter`: `100 * (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`. Coarse (page cache keeps it high), so secondary to the buffer-pool ratio above.


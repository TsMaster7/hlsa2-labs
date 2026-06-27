### 1. QPS Calculation
 - **total number of requests per day (average):** dau * sessions_per_user * requests_per_session = 1000000 * 5 * 30 = **150000000**
 - **average QPS:** 150000000 / 86400 = **1736**
 - **peak QPS:** averageQPS * burst_factor = 1736 * 4 = **6944**
 - **internal write QPS:** (1 / (read_write_ratio + 1)) * averageQPS * replication_factor = (1 / 10) * 1736 * 3 = **521**
 - **peak internal write QPS:** 521 * 4 = **2084**

**Sanity check — peak QPS**

We need to introduce new metrics (either measured or estimated some other way) to make an alternative calculation.
Re-estimate the peak QPS from independent assumptions: 
 - at peak ~3% of DAU are online (`peak_online_fraction`),
 - each sending 1 request every 5s (`think_time_s`).

So **peak QPS (per-second envelope) can be calculated as:** peak_online_fraction * dau * (1 / think_time_s) = 0.03 * 1000000 * (1 / 5) = **6000**

~6000 vs 6944 (~13% apart) → same order of magnitude, so burst_factor = 4 is a reasonable assumption. (φ and think_time are assumptions, not measured.)

### 2. Storage projection
Use **internal write QPS = 521** from section 1 — it already includes the replication_factor, so replication is accounted for at the daily level (every write is replicated each day).
 - **raw bytes/day (replicated):** internal_write_QPS * payload_bytes * 86400 = 521 * 4096 * 86400 ≈ **184.32 GB/day** (= 45,000,000 replicated writes/day * 4096 B)
 - **+30% index/metadata:** 184.32 * 1.3 = **239.62 GB/day**
 - **physical at 12 mo (360 d):** 239.62 * 360 = **86.26 TB**
 - **physical at 24 mo (720 d):** 239.62 * 720 = **172.52 TB**
 - **+30% headroom (compaction, hotspots, reindex):**
   - 12 mo: 86.26 * 1.3 = **112.14 TB**
   - 24 mo: 172.52 * 1.3 = **224.28 TB**

### 3. Bandwidth budget
All flows at peak. payload = 4096 B, peak_total_qps = 6944, peak_read_qps = 6250, peak_write_qps = 694 (MB = 1024^2 B).

| Flow | Direction | Formula | Value |
|------|-----------|---------|-------|
| Ingress (all requests in) | inbound | peak_total_qps * payload = 6944 * 4096 | **27.1 MB/s** |
| Egress (responses to clients) | outbound | peak_read_qps * payload = 6250 * 4096 | **24.4 MB/s** |
| Replication egress | outbound | peak_write_qps * payload * (RF - 1) = 694 * 4096 * 2 | **5.4 MB/s** |
| Fan-out egress (index, cache, audit) | outbound | peak_write_qps * payload * fan_out = 694 * 4096 * 2 | **5.4 MB/s** |
| **Total egress** | outbound | 24.4 + 5.4 + 5.4 | **35.2 MB/s** |

**NIC check (70% utilisation ceiling):**
 - 1 Gbps NIC = 125 MB/s → 70% = 87.5 MB/s → 35.2 / 87.5 ≈ **40% used** ✓
 - 10 Gbps NIC = 1250 MB/s → 70% = 875 MB/s → 35.2 / 875 ≈ **4% used** ✓

→ Bandwidth is not the bottleneck; a single 1 Gbps NIC per node is enough.

### 4. Working-set memory (RAM per node)
No simulator backing — all inputs below are assumptions (like the NIC rule). Cluster: 3 nodes, RF = 3 (each node holds a full copy).
Shared assumptions / definitions:
 - **total_daily_payload = 61.44 GB** — one day of written data, single copy (a node keeps only its own copy in RAM, so no replication here). See note below for the breakdown.
 - **memory_capacity_index = 1.3** — index/metadata overhead carried in RAM (same +30% as on disk)
 - **hot window = 12h** (recent data is the data actually read for the last 12 hours – just an assumption)
 - **peak concurrent connections = 30,000** (from section 1: peak_online_fraction * dau), so 10,000 per node
 - **per-connection working set = 4 * payload_bytes = 16 KB** (inbound + outbound + scratch)

| Component | What it is | Formula                                                                                         | Per node |
|-----------|-----------|-------------------------------------------------------------------------------------------------|----------|
| Hot data (DB buffer pool) | recent data kept resident so reads avoid disk | daily_write_payload * (12/24) * memory_capacity_index = 61.44 * 0.5 * 1.3                       | **≈ 40 GB** |
| Connection buffers | per-request processing memory for open connections | per_connection_working_set * (peak_concurrent_connections / nodes) = (4 * 4096 B) * (30000 / 3) | **≈ 0.16 GB** |
| Read cache | separate cache tier sized to serve the 80% hit ratio | cache_hit_ratio * hot_data = 0.80 * 40                                                          | **≈ 32 GB** |
| **Total** | working-set RAM per node | 40 + 0.16 + 32                                                                                  | **≈ 72 GB** |
**Where:**

- **daily_write_payload:** it's one day of writes, single copy, before indexes (same base as section 2):
  - daily_writes = (1 / (read_write_ratio + 1)) * daily_requests = 0.1 * 150,000,000 = 15,000,000
  - daily_write_payload = daily_writes * payload_bytes = 15,000,000 * 4096 ≈ **61.44 GB**

- **per_connection_working_set:**
  - **per_connection_working_set = multiplicator * payload_bytes = 4 * 4096 = 16 KB** — RAM one open connection needs to process a request (inbound payload + outbound response + scratch (parsed objects, serialize buffer, ...) - may give us **multiplicator** = 4, – just an assumption).
  - **peak_concurrent_connections = 30,000** — connections open at the peak instant. Reused from the [section 1](#1-qps-calculation) sanity check: `peak_online_fraction * dau`.
   - **peak_online_fraction = 0.03 (3%)** — assumption introduced in the [section 1](#1-qps-calculation) sanity check (share of DAU online at peak).
   - → `16 KB * 10,000 ≈ 0.16 GB per node`. (Note: this is a *count* of open connections × bytes each — not QPS × bytes, which would be bandwidth.)

**RAM check:** a 128 GB node fits this at ~56% → memory is comfortable, not the bottleneck.

### 5. Capacity: nodes & buffer

**Effective storage QPS** — the load that actually reaches the storage tier at peak (cache absorbs reads, write amplification inflates writes):
 - effective_storage_read = peak_read_qps * (1 − cache_hit_ratio) = 6250 * (1 − 0.80) = **1250 req/s**
 - effective_storage_write = peak_write_qps * write_amplification = 694 * 1.4 = **972 req/s**
 - **storage_qps = 1250 + 972 = 2222 req/s**

**Where does the buffer come from?** The buffer is not an arbitrary constant — it is set by the **slowest tier to scale**, because demand keeps growing during the whole scale-up window:

`buffer = slowest_tier_time_to_scale × max_growth_rate + safety_margin → target_utilisation`

 - **Slowest tier = storage.** Stateless QPS nodes scale in minutes; rebalancing/backfilling a replicated dataset is days–weeks. So storage's scale time (~**2 weeks**) sets the policy for the whole cluster.
 - **Policy (band), the binding number:** a 2-week-to-scale tier maps to **60–70% target utilisation (30–40% buffer)**. The config's `target_buffer_pct = 40` → **60% target utilisation = the conservative end of the storage band**. ✓
 - **Growth (sanity-check floor), from [section 2](#2-storage-projection):** daily growth ≈ 239.62 GB/day → ≈ 7.0 TB/month → ≈ 8.1%/month of the 12-mo base (86.26 TB). Over a 2-week scale window that is `0.5 × 7.0 ≈ 3.5 TB ≈ 4.1%` of base; even with a safety margin (~8–10%) this sits **well inside the 40% buffer**. → organic growth is *not* what forces the buffer; the band (spike absorption + scale-time variance + safety) dominates, and 40% is comfortable.

**Node count** (at 60% target utilisation):
 - effective_node_qps (at buffer) = instance_qps_capacity * (1 − target_buffer_pct/100) = 5000 * (1 − 0.40) = **3000 req/s**
 - nodes_for_throughput = ⌈2222 / 3000⌉ = **1**
 - nodes_for_durability = replication_factor = **3**
 - **nodes_required = max(1, 3) = 3** → the cluster is **durability-bound, not throughput-bound**

**Buffer / headroom (realised):**
 - provisioned_qps = nodes_required * instance_qps_capacity = 3 * 5000 = **15,000 req/s**
 - utilisation at peak = 2222 / 15000 = **14.8%**
 - **headroom at peak = 85.2%** (target ≥ 40%) ✓ — far above target, throughput is not a problem at all.


### 6. Cost
 - **monthly compute** = nodes_required * cost_per_node_per_month = 3 * 350$ = **1,050\$**
 - **monthly hot storage** = storage_at_12mo(GB) * $0.10/GB (we are calculating the last month's storage). 
   - Using the section-2 estimate (86.26 TB ≈ 88,330 GB): 88,330 * 0.10 ≈ **$8,833**
 - **monthly TOTAL** ≈ 1,050 + 8,833 = **9,883\$**
 - **cost per 1k QPS** = monthly_total / (peak_total_qps / 1000) = 9,883 / (6944 / 1000) ≈ **1,423\$**

→ **Storage dominates (~89% of the bill); compute is only ~11%.** Note cost-per-1k-QPS is normalised against peak *total* QPS (6944), not effective storage QPS.



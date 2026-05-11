# Labs Index

| Lab | Title | Topic |
|-----|-------|-------|
| [1-1](1-1-latency-throughput-scaling-laws/) | Latency, Throughput, and Scaling Laws | Foundations, Workloads, and Capacity |
| [1-2](1-2-workload-characterization-bottleneck-analysis/) | Workload Characterization and Bottleneck Analysis | Arrival Patterns, R/W Ratios, and Utilisation |
| [1-3](1-3-capacity-estimation-back-of-envelope/) | Capacity Estimation (Back of Envelope) | Capacity Modelling, Multipliers, and Cost |
| [2-2](2-2-red-use-sli-slo-alert-quality/) | RED, USE, SLIs, SLOs, and Alert Quality | Observability, SLOs, and Burn-Rate Alerts |
| [2-3](2-3-load-testing-stress-testing-benchmark-methodology/) | Load Testing, Stress Testing, and Benchmark Methodology | Open-Loop Load Testing, Coordinated Omission, Regression |

Each lab folder contains its own `README.md` with full setup, run, and
deliverable instructions. The labs come in two flavours:

- **Python simulator labs (1-1, 1-2, 1-3).** A self-contained Python
  package, a benchmark script under `scripts/`, automated tests under
  `tests/`, and a `results/` directory where you commit your benchmark
  output and analysis.
- **Containerised stack labs (2-2, 2-3).** A Docker Compose stack
  (service under test, load generator, Prometheus, Grafana, and friends)
  plus `docs/`, `runbooks/`, and per-lab artefact directories where you
  commit your SLO/workload designs, experiment evidence, and architecture
  reviews.

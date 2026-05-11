# Highload Software Architecture 2 — Labs

Hands-on labs for the **Highload Software Architecture 2** course.
Each lab is a self-contained project you run locally — either a Python
simulator (labs 1-x) or a Docker Compose stack with a real service
under test, load generator, and observability tooling (labs 2-x) — that
you benchmark, improve, and document.

## Labs

| #   | Directory | Topic |
|-----|-----------|-------|
| 1-1 | [labs/1-1-latency-throughput-scaling-laws](labs/1-1-latency-throughput-scaling-laws/) | Latency, Throughput, and Scaling Laws |
| 1-2 | [labs/1-2-workload-characterization-bottleneck-analysis](labs/1-2-workload-characterization-bottleneck-analysis/) | Workload Characterization and Bottleneck Analysis |
| 1-3 | [labs/1-3-capacity-estimation-back-of-envelope](labs/1-3-capacity-estimation-back-of-envelope/) | Capacity Estimation (Back of Envelope) |
| 2-2 | [labs/2-2-red-use-sli-slo-alert-quality](labs/2-2-red-use-sli-slo-alert-quality/) | RED, USE, SLIs, SLOs, and Alert Quality |
| 2-3 | [labs/2-3-load-testing-stress-testing-benchmark-methodology](labs/2-3-load-testing-stress-testing-benchmark-methodology/) | Load Testing, Stress Testing, and Benchmark Methodology |

## Getting Started

1. **Fork** this repository on GitHub so you have your own copy.
2. **Clone** your fork locally:

```bash
gh repo fork kirill-latish/hlsa2-labs --clone
cd hlsa2-labs
```

3. Navigate to the lab directory and follow the lab's `README.md`.

## Requirements

- Python 3.12+ (labs 1-1, 1-2, 1-3)
- Docker 24+ and Docker Compose v2 (labs 2-2, 2-3)
- [k6](https://k6.io/docs/getting-started/installation/) 0.50+ on PATH (lab 2-3)
- Git

Each lab's `README.md` lists the exact tooling and version it expects.

## Repository Structure

```
hlsa2-labs/
  README.md          ← you are here
  labs/
    README.md        ← labs index
    1-1-latency-throughput-scaling-laws/
      README.md      ← lab setup, how to run, config reference
      simulator/     ← Python package (the code you benchmark and improve)
      scripts/       ← benchmark runner
      tests/         ← automated tests
      results/       ← your benchmark output (committed by you)
    1-2-workload-characterization-bottleneck-analysis/
      README.md      ← lab setup, how to run, config reference
      simulator/     ← Python package (arrival patterns, R/W workloads)
      scripts/       ← benchmark runner
      tests/         ← automated tests
      results/       ← your benchmark output (committed by you)
    1-3-capacity-estimation-back-of-envelope/
      README.md      ← lab setup, how to run, config reference
      simulator/     ← Python package (workload + storage + network + capacity + cost)
      scripts/       ← benchmark runner
      tests/         ← automated tests
      results/       ← your benchmark output (committed by you)
    2-2-red-use-sli-slo-alert-quality/
      README.md      ← lab setup, stack overview, experiment workflow
      docker-compose.yml ← checkout + payments + loadgen + Prometheus + Alertmanager + Grafana
      checkout/      ← FastAPI service under SLO
      payments/      ← downstream stub with fault-injection admin API
      loadgen/       ← scripted offered-load + fault profiles
      prometheus/    ← scrape config + your recording/alerting rules
      alertmanager/  ← routing for severity: page / ticket
      grafana/       ← provisioned RED + Burn Rate dashboard
      runbooks/      ← incident runbook(s) you fill in
      docs/          ← your SLO design and architecture review (committed by you)
      artifacts/     ← experiment evidence (committed by you)
      scripts/       ← helper scripts
    2-3-load-testing-stress-testing-benchmark-methodology/
      README.md      ← lab setup, stack overview, experiment workflow
      docker-compose.yml ← sut (Go) + downstream + Postgres + Prometheus + Grafana + exporters
      Makefile       ← `make run-sut`, `make perf-baseline`, etc.
      sut/           ← Go HTTP service under test
      downstream/    ← Go downstream stub
      postgres/      ← Postgres init + tuning
      prometheus/    ← scrape config and rules
      grafana/       ← provisioned dashboards
      perf/          ← k6 scripts, workload model, results (committed by you)
      runbooks/      ← rollback runbook(s) you fill in
      docs/          ← coordinated-omission analysis + architecture review (committed by you)
      scripts/       ← regression / coordinated-omission analyzers
```

## License

Educational use only. All rights reserved.

"""Benchmark runner — loads config, generates the workload, runs it
through the worker pool, computes resource utilisation, and prints a
formatted report to stdout."""

from __future__ import annotations

import os
import time
from pathlib import Path

import yaml

from simulator.metrics import MetricsCollector
from simulator.worker_pool import WorkerPool
from simulator.workload import generate_arrivals, generate_requests

_SEPARATOR = "=" * 60


def load_config(config_path: str | Path | None = None) -> dict:
    if config_path is None:
        config_path = Path(__file__).resolve().parent.parent / "config.yaml"
    config_path = Path(config_path)
    with open(config_path) as f:
        return yaml.safe_load(f)


def _compute_mean_interval(
    target_util: float,
    workers: int,
    io_workers: int,
    service_time_ms: float,
    write_service_time_ms: float,
    read_fraction: float,
) -> float:
    """Derive the mean inter-arrival time (ms) that produces *target_util*
    on the bottleneck resource.

    We compute the arrival rate that would produce *target_util* on each
    resource and pick the slower (lower) rate — i.e. the one that hits
    the target first.
    """
    avg_svc_s = (
        read_fraction * service_time_ms
        + (1.0 - read_fraction) * write_service_time_ms
    ) / 1000.0

    # Connection pool: util = rps * avg_svc_s / workers
    rps_conn = target_util * workers / max(avg_svc_s, 1e-9)

    # I/O subsystem: util = write_rps * write_svc_s / io_workers
    #                write_rps = rps * (1 - read_fraction)
    write_svc_s = write_service_time_ms / 1000.0
    write_frac = 1.0 - read_fraction
    if write_frac > 0:
        rps_io = target_util * io_workers / (write_svc_s * write_frac)
    else:
        rps_io = float("inf")

    target_rps = min(rps_conn, rps_io)
    return 1000.0 / max(target_rps, 1e-9)


def run_benchmark(
    config_path: str | Path | None = None,
    *,
    arrival_pattern: str | None = None,
    read_fraction: float | None = None,
    target_utilization: float | None = None,
) -> list[dict]:
    """Run the benchmark and return a list with one result dict."""
    cfg = load_config(config_path)

    service_time_ms: float = cfg.get("service_time_ms", 10)
    write_service_time_ms: float = cfg.get("write_service_time_ms", 15)
    workers: int = cfg.get("workers", os.cpu_count() or 4)
    io_workers_cfg: int = cfg.get("io_workers", 2)
    total_requests: int = cfg.get("total_requests", 500)

    pattern = arrival_pattern or cfg.get("arrival_pattern", "poisson")
    rf = read_fraction if read_fraction is not None else cfg.get("read_fraction", 0.7)
    tu = target_utilization if target_utilization is not None else cfg.get("target_utilization", 0.75)
    burst_cv: float = cfg.get("burst_cv", 2.0)
    replica_lag_ms: float = cfg.get("replica_lag_ms", 0.0)

    # --- Print config ---------------------------------------------------
    cqrs_label = f"replica_lag={replica_lag_ms}ms" if replica_lag_ms > 0 else "cqrs=off"
    print(
        f"Config: service_time={service_time_ms}ms, "
        f"write_service_time={write_service_time_ms}ms, "
        f"workers={workers}, io_workers={io_workers_cfg}, {cqrs_label}"
    )
    print(
        f"        arrival={pattern}, read_fraction={rf:.2f}, "
        f"target_util={tu:.2f}, burst_cv={burst_cv}"
    )
    print()

    # --- Generate workload ----------------------------------------------
    mean_interval = _compute_mean_interval(
        tu, workers, io_workers_cfg,
        service_time_ms, write_service_time_ms, rf,
    )
    arrivals = generate_arrivals(total_requests, pattern, mean_interval, burst_cv)
    requests = generate_requests(total_requests, rf)

    # --- Run -------------------------------------------------------------
    pool = WorkerPool(
        workers=workers,
        service_time_ms=service_time_ms,
        write_service_time_ms=write_service_time_ms,
        replica_lag_ms=replica_lag_ms,
    )
    collector = MetricsCollector()

    for interval in arrivals:
        collector.record_inter_arrival(interval)

    start = time.monotonic()
    pool.run(requests, collector, inter_arrival_times=arrivals)
    duration_s = time.monotonic() - start

    summary = collector.summary(
        duration_s,
        workers=workers,
        io_workers=io_workers_cfg,
        read_fraction=rf,
        service_time_ms=service_time_ms,
        write_service_time_ms=write_service_time_ms,
    )
    summary["duration_s"] = round(duration_s, 3)
    summary["arrival_pattern"] = pattern

    _print_report(summary)
    return [summary]


def _print_report(s: dict) -> None:
    """Print a human-readable benchmark report."""

    # Arrival statistics
    print("Arrival statistics:")
    print(f"  inter_arrival_mean_ms: {s['inter_arrival_mean_ms']:.2f}")
    print(f"  inter_arrival_std_ms:  {s['inter_arrival_std_ms']:.2f}")
    print(f"  CV: {s['cv']:.2f}")
    print()

    # Main results table
    print(_SEPARATOR)
    print("BENCHMARK RESULTS")
    print(_SEPARATOR)
    print()

    header = (
        f"{'Requests':>8} {'Reads':>7} {'Writes':>7} {'Rejected':>8} "
        f"{'Mean(ms)':>9} {'p95(ms)':>9} {'Throughput':>12} {'Duration':>10}"
    )
    print(header)
    print("-" * len(header))
    print(
        f"{s['count']:>8} {s['read_count']:>7} {s['write_count']:>7} "
        f"{s['rejected']:>8} {s['mean_ms']:>9.2f} {s['p95_ms']:>9.2f} "
        f"{s['throughput_rps']:>10.2f} r/s {s['duration_s']:>8.3f} s"
    )
    print()

    # Resource utilisation
    print("Resource utilisation:")
    print(f"  connection_pool:  {s['conn_pool_util']:.1f}%")
    print(f"  io_subsystem:     {s['io_util']:.1f}%")
    print(f"  bottleneck:       {s['bottleneck_resource']} ({s['bottleneck_util']:.1f}%)")
    print()

    # Per-type latency breakdown
    print(f"Read latency:  mean={s['read_mean_ms']:.2f}ms  p95={s['read_p95_ms']:.2f}ms")
    print(f"Write latency: mean={s['write_mean_ms']:.2f}ms  p95={s['write_p95_ms']:.2f}ms")
    print()
    print(_SEPARATOR)

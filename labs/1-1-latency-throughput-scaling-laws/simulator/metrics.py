"""Thread-safe metrics collector for latency and throughput measurement."""

from __future__ import annotations

import statistics
import threading


class MetricsCollector:
    """Collects per-request latencies in a thread-safe manner and computes
    summary statistics (mean, p95, throughput)."""

    def __init__(self) -> None:
        self._latencies: list[float] = []
        self._queue_times: list[float] = []
        self._service_times: list[float] = []
        self._lock = threading.Lock()
        self._rejected = 0

    def record(self, latency_ms: float) -> None:
        with self._lock:
            self._latencies.append(latency_ms)

    def record_times(self, queue_time_ms: float, service_time_ms: float) -> None:
        """Record queue wait time and service time separately."""
        with self._lock:
            self._queue_times.append(queue_time_ms)
            self._service_times.append(service_time_ms)
            self._latencies.append(queue_time_ms + service_time_ms)

    def record_rejection(self) -> None:
        with self._lock:
            self._rejected += 1

    @property
    def count(self) -> int:
        with self._lock:
            return len(self._latencies)

    @property
    def rejected(self) -> int:
        with self._lock:
            return self._rejected

    def mean(self) -> float:
        with self._lock:
            if not self._latencies:
                return 0.0
            return statistics.mean(self._latencies)

    def p95(self) -> float:
        with self._lock:
            if not self._latencies:
                return 0.0
            sorted_lat = sorted(self._latencies)
            idx = int(len(sorted_lat) * 0.95)
            idx = min(idx, len(sorted_lat) - 1)
            return sorted_lat[idx]

    def throughput(self, duration_s: float) -> float:
        with self._lock:
            if duration_s <= 0:
                return 0.0
            return len(self._latencies) / duration_s

    def summary(self, duration_s: float) -> dict:
        with self._lock:
            if not self._latencies:
                return {
                    "mean_ms": 0.0,
                    "p95_ms": 0.0,
                    "queue_mean_ms": 0.0,
                    "queue_p95_ms": 0.0,
                    "service_mean_ms": 0.0,
                    "service_p95_ms": 0.0,
                    "count": 0,
                    "rejected": self._rejected,
                    "throughput_rps": 0.0,
                }
            sorted_lat = sorted(self._latencies)
            idx = min(int(len(sorted_lat) * 0.95), len(sorted_lat) - 1)

            result = {
                "mean_ms": round(statistics.mean(self._latencies), 2),
                "p95_ms": round(sorted_lat[idx], 2),
                "count": len(self._latencies),
                "rejected": self._rejected,
                "throughput_rps": round(len(self._latencies) / max(duration_s, 1e-9), 2),
            }

            # Add queue and service time metrics if available
            if self._queue_times:
                sorted_queue = sorted(self._queue_times)
                queue_idx = min(int(len(sorted_queue) * 0.95), len(sorted_queue) - 1)
                result["queue_mean_ms"] = round(statistics.mean(self._queue_times), 2)
                result["queue_p95_ms"] = round(sorted_queue[queue_idx], 2)
            else:
                result["queue_mean_ms"] = 0.0
                result["queue_p95_ms"] = 0.0

            if self._service_times:
                sorted_service = sorted(self._service_times)
                service_idx = min(int(len(sorted_service) * 0.95), len(sorted_service) - 1)
                result["service_mean_ms"] = round(statistics.mean(self._service_times), 2)
                result["service_p95_ms"] = round(sorted_service[service_idx], 2)
            else:
                result["service_mean_ms"] = 0.0
                result["service_p95_ms"] = 0.0

            return result

    def reset(self) -> None:
        with self._lock:
            self._latencies.clear()
            self._rejected = 0

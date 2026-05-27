"""Worker pool that processes simulated read/write requests.

The baseline implementation has NO rate limiting, NO back-pressure,
and NO request routing (reads and writes share the same pool).
This is intentional — the workload sweep will show where the
bottleneck forms.  Students improve this in their homework.

CQRS improvement: set replica_lag_ms > 0 to enable read/write routing.
Reads are dispatched to a dedicated replica pool (service_time_ms base
cost + replica_lag_ms network overhead); writes stay on the primary pool
(write_service_time_ms).  The two pools are sized identically (workers
threads each) so reads and writes no longer compete for the same workers.
"""

from __future__ import annotations

import random
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import TYPE_CHECKING

from simulator.workload import route_request

if TYPE_CHECKING:
    from simulator.metrics import MetricsCollector
    from simulator.workload import Request


class WorkerPool:
    """Fixed-size thread pool that simulates request processing with
    differentiated read/write service times.

    When *replica_lag_ms* > 0 the pool operates in CQRS mode: reads go
    to a dedicated replica executor, writes to the primary executor.
    """

    def __init__(
        self,
        workers: int,
        service_time_ms: float,
        write_service_time_ms: float | None = None,
        replica_lag_ms: float = 0.0,
    ) -> None:
        self.workers = workers
        self.service_time_ms = service_time_ms
        self.write_service_time_ms = write_service_time_ms or service_time_ms
        self.replica_lag_ms = replica_lag_ms

    def _handle_request(
        self,
        request: Request,
        submit_time: float,
        replica: bool = False,
    ) -> tuple[float, str]:
        """Simulate processing a single request.

        Returns ``(latency_ms, request_type)``.
        Latency is measured from submission (includes queue wait time + service time),
        which is what reveals queueing effects under bursty arrivals.
        Adds +/-20 % jitter to the base service time.

        When *replica* is True the request runs on the replica pool:
        service cost is service_time_ms (reads avoid WAL/write-lock overhead)
        plus replica_lag_ms (simulated network round-trip to the replica).
        """
        if replica:
            jitter = random.uniform(0.8, 1.2)
            sleep_s = (self.service_time_ms * jitter + self.replica_lag_ms) / 1000.0
        else:
            base_ms = (
                self.write_service_time_ms
                if request.type == "write"
                else self.service_time_ms
            )
            jitter = random.uniform(0.8, 1.2)
            sleep_s = (base_ms * jitter) / 1000.0

        time.sleep(sleep_s)
        latency_ms = (time.monotonic() - submit_time) * 1000.0
        return latency_ms, request.type

    def run(
        self,
        requests: list[Request],
        collector: MetricsCollector,
        inter_arrival_times: list[float] | None = None,
    ) -> None:
        """Submit requests and record latencies.

        When *inter_arrival_times* is provided, requests are spaced
        according to the given intervals (in ms).  Otherwise all
        requests are submitted as fast as possible.

        CQRS mode is active when replica_lag_ms > 0.
        """
        if self.replica_lag_ms > 0:
            self._run_cqrs(requests, collector, inter_arrival_times)
        else:
            self._run_shared(requests, collector, inter_arrival_times)

    def _run_shared(
        self,
        requests: list[Request],
        collector: MetricsCollector,
        inter_arrival_times: list[float] | None = None,
    ) -> None:
        """Baseline: all requests share one pool."""
        with ThreadPoolExecutor(max_workers=self.workers) as pool:
            futures: dict = {}
            for i, req in enumerate(requests):
                submit_time = time.monotonic()
                futures[pool.submit(self._handle_request, req, submit_time)] = req
                if inter_arrival_times is not None and i < len(inter_arrival_times):
                    time.sleep(inter_arrival_times[i] / 1000.0)

            for future in as_completed(futures):
                latency_ms, req_type = future.result()
                collector.record(latency_ms, req_type)

    def _run_cqrs(
        self,
        requests: list[Request],
        collector: MetricsCollector,
        inter_arrival_times: list[float] | None = None,
    ) -> None:
        """CQRS: reads → replica pool, writes → primary pool.

        Both pools have self.workers threads, so reads and writes scale
        independently and no longer contend for the same workers.
        """
        with (
            ThreadPoolExecutor(max_workers=self.workers) as primary,
            ThreadPoolExecutor(max_workers=self.workers) as replica,
        ):
            futures: dict = {}
            for i, req in enumerate(requests):
                submit_time = time.monotonic()
                is_replica = route_request(req, cqrs_enabled=True) == "replica"
                target = replica if is_replica else primary
                futures[target.submit(self._handle_request, req, submit_time, is_replica)] = req
                if inter_arrival_times is not None and i < len(inter_arrival_times):
                    time.sleep(inter_arrival_times[i] / 1000.0)

            for future in as_completed(futures):
                latency_ms, req_type = future.result()
                collector.record(latency_ms, req_type)

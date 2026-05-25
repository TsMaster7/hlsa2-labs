"""Worker pool that processes simulated read/write requests.

The baseline implementation has NO rate limiting, NO back-pressure,
and NO request routing (reads and writes share the same pool).
This is intentional — the workload sweep will show where the
bottleneck forms.  Students improve this in their homework.
"""

from __future__ import annotations

import random
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from simulator.metrics import MetricsCollector
    from simulator.workload import Request


class WorkerPool:
    """Fixed-size thread pool that simulates request processing with
    differentiated read/write service times."""

    def __init__(
        self,
        workers: int,
        service_time_ms: float,
        write_service_time_ms: float | None = None,
    ) -> None:
        self.workers = workers
        self.service_time_ms = service_time_ms
        self.write_service_time_ms = write_service_time_ms or service_time_ms

    def _handle_request(self, request: Request, submit_time: float) -> tuple[float, str]:
        """Simulate processing a single request.

        Returns ``(latency_ms, request_type)``.
        Latency is measured from submission (includes queue wait time + service time),
        which is what reveals queueing effects under bursty arrivals.
        Adds +/-20 % jitter to the base service time.
        """
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
        """
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

"""Workload generators for arrival patterns and request types.

This is the file you will improve.  The baseline has no rate limiting,
no back-pressure, and no request routing.  See the improvement options
in the homework task description.
"""

from __future__ import annotations

import math
import random
from dataclasses import dataclass


@dataclass
class Request:
    """A single benchmark request with a read/write type."""

    id: int
    type: str  # "read" or "write"


def generate_requests(total: int, read_fraction: float) -> list[Request]:
    """Generate a mix of read and write requests.

    Each request is independently assigned as a read with probability
    *read_fraction* or a write otherwise.
    """
    requests: list[Request] = []
    for i in range(1, total + 1):
        req_type = "read" if random.random() < read_fraction else "write"
        requests.append(Request(id=i, type=req_type))
    return requests


def generate_arrivals(
    total: int,
    pattern: str,
    mean_interval_ms: float,
    burst_cv: float = 2.0,
) -> list[float]:
    """Generate inter-arrival times in milliseconds.

    *total* is the number of requests; the returned list has length
    ``total - 1`` (one interval between each consecutive pair).

    Supported patterns:

    * ``poisson``  -- exponential inter-arrivals, CV ≈ 1.0
    * ``bursty``   -- log-normal inter-arrivals, CV ≈ *burst_cv*
    * ``regular``  -- near-constant intervals, CV ≈ 0.1
    """
    if total <= 1:
        return []

    count = total - 1

    if pattern == "poisson":
        rate = 1.0 / mean_interval_ms
        return [random.expovariate(rate) for _ in range(count)]

    if pattern == "bursty":
        # Log-normal parameterisation so that E[X] = mean_interval_ms
        # and CV = burst_cv.
        #   CV² = exp(σ²) − 1  →  σ² = ln(1 + CV²)
        #   E[X] = exp(μ + σ²/2) →  μ = ln(mean) − σ²/2
        sigma_sq = math.log(1.0 + burst_cv ** 2)
        sigma = math.sqrt(sigma_sq)
        mu = math.log(mean_interval_ms) - sigma_sq / 2.0
        return [random.lognormvariate(mu, sigma) for _ in range(count)]

    if pattern == "regular":
        # Near-constant intervals with small Gaussian jitter (CV ≈ 0.1).
        return [
            max(mean_interval_ms * random.gauss(1.0, 0.1), 0.01)
            for _ in range(count)
        ]

    raise ValueError(f"Unknown arrival pattern: {pattern!r}")


def route_request(request: Request, cqrs_enabled: bool) -> str:
    """Return the target pool for a request under CQRS routing.

    Reads go to the replica pool (lower service cost, no write-lock
    contention); writes always go to the primary.  When *cqrs_enabled*
    is False every request is routed to ``'primary'``.
    """
    if cqrs_enabled and request.type == "read":
        return "replica"
    return "primary"

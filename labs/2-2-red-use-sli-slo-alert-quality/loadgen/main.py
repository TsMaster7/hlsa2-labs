"""Load generator with three fault-injection profiles.

Drives a constant offered load against the checkout service and toggles
the payments stub's fault injection at the start and end of the run.
This mirrors a controlled experiment: a clean baseline followed by an
injected, time-bounded fault that the SLO alerts must catch.

Usage (inside the loadgen container):

    python -m loadgen nominal
    python -m loadgen fast-burn
    python -m loadgen slow-burn

Or list profiles:

    python -m loadgen --list

Profiles are defined in ``loadgen/profiles.yaml``. Each profile has:

    rps:                  steady offered load
    duration_seconds:     how long to drive load
    payments:             fault settings POSTed to /admin/inject before the run
        fail_ratio
        latency_ms
        latency_jitter_ms
    description:          human-readable summary

A separate /metrics endpoint on port 9999 lets Prometheus record the
offered load (loadgen_requests_total, loadgen_request_duration_seconds)
independently of the checkout service.
"""

from __future__ import annotations

import argparse
import asyncio
import os
import random
import sys
import time
import uuid
from pathlib import Path
from typing import Any

import httpx
import yaml
from prometheus_client import Counter, Histogram, start_http_server

CHECKOUT_URL = os.environ.get(
    "CHECKOUT_URL", "http://checkout:8080/checkout"
)
PAYMENTS_ADMIN_URL = os.environ.get(
    "PAYMENTS_ADMIN_URL", "http://payments:8081/admin/inject"
)
METRICS_PORT = int(os.environ.get("LOADGEN_METRICS_PORT", "9999"))
PROFILES_PATH = Path(
    os.environ.get(
        "LOADGEN_PROFILES_PATH", "/app/loadgen/profiles.yaml"
    )
)

loadgen_requests_total = Counter(
    "loadgen_requests_total",
    "Total HTTP requests issued by the load generator.",
    labelnames=("profile", "status_class"),
)

loadgen_request_duration_seconds = Histogram(
    "loadgen_request_duration_seconds",
    "Client-side request duration in seconds, as observed by the load generator.",
    labelnames=("profile",),
    buckets=(0.05, 0.1, 0.2, 0.3, 0.5, 0.8, 1.0, 1.5, 2.0, 3.0, 5.0),
)


def _status_class(status_code: int) -> str:
    if 200 <= status_code < 300:
        return "2xx"
    if 300 <= status_code < 400:
        return "3xx"
    if 400 <= status_code < 500:
        return "4xx"
    if 500 <= status_code < 600:
        return "5xx"
    return "other"


def load_profiles() -> dict[str, dict[str, Any]]:
    if not PROFILES_PATH.exists():
        raise SystemExit(
            f"Profiles file not found at {PROFILES_PATH}. "
            "Set LOADGEN_PROFILES_PATH or mount loadgen/profiles.yaml."
        )
    with PROFILES_PATH.open("r", encoding="utf-8") as f:
        data = yaml.safe_load(f) or {}
    profiles = data.get("profiles") or {}
    if not isinstance(profiles, dict) or not profiles:
        raise SystemExit("profiles.yaml must define a non-empty 'profiles' map")
    return profiles


async def apply_payments_injection(
    client: httpx.AsyncClient, payload: dict[str, Any]
) -> None:
    try:
        r = await client.post(PAYMENTS_ADMIN_URL, json=payload, timeout=5.0)
        r.raise_for_status()
        print(
            f"[loadgen] payments injection set: {payload} -> {r.json()}",
            flush=True,
        )
    except Exception as exc:  # noqa: BLE001 - intentional broad log
        print(
            f"[loadgen] WARNING: failed to apply payments injection: {exc}",
            flush=True,
        )


async def fire_one(
    client: httpx.AsyncClient, profile_name: str
) -> None:
    request_id = str(uuid.uuid4())
    start = time.perf_counter()
    try:
        r = await client.post(
            CHECKOUT_URL,
            json={"trace": "loadgen"},
            headers={"x-request-id": request_id},
            timeout=5.0,
        )
        status = r.status_code
    except httpx.TimeoutException:
        status = 504
    except httpx.HTTPError:
        status = 599
    elapsed = time.perf_counter() - start
    loadgen_request_duration_seconds.labels(profile=profile_name).observe(elapsed)
    loadgen_requests_total.labels(
        profile=profile_name, status_class=_status_class(status)
    ).inc()


async def run_profile(profile_name: str, profile: dict[str, Any]) -> None:
    rps = float(profile.get("rps", 50))
    duration = float(profile.get("duration_seconds", 60))
    payments = profile.get("payments") or {
        "fail_ratio": 0.0,
        "latency_ms": 30,
        "latency_jitter_ms": 20,
    }
    description = profile.get("description", "")
    if rps <= 0:
        raise SystemExit("rps must be > 0")

    print(
        f"[loadgen] profile={profile_name!r} rps={rps} duration_s={duration} "
        f"payments={payments} description={description!r}",
        flush=True,
    )

    interval = 1.0 / rps
    deadline = time.monotonic() + duration

    async with httpx.AsyncClient() as client:
        await apply_payments_injection(client, payments)

        try:
            tasks: list[asyncio.Task[None]] = []
            next_send = time.monotonic()
            while time.monotonic() < deadline:
                task = asyncio.create_task(fire_one(client, profile_name))
                tasks.append(task)
                next_send += interval
                # mild jitter so we don't synchronise across processes
                sleep_for = max(
                    0.0,
                    next_send - time.monotonic() + random.uniform(-0.0005, 0.0005),
                )
                await asyncio.sleep(sleep_for)
                # reap completed tasks periodically
                if len(tasks) > int(rps * 5):
                    tasks = [t for t in tasks if not t.done()]
            await asyncio.gather(*tasks, return_exceptions=True)
        finally:
            print(
                f"[loadgen] profile={profile_name!r} done; resetting payments to baseline",
                flush=True,
            )
            await apply_payments_injection(
                client,
                {"fail_ratio": 0.0, "latency_ms": 30, "latency_jitter_ms": 20},
            )


def main() -> None:
    parser = argparse.ArgumentParser(prog="loadgen")
    parser.add_argument("profile", nargs="?", help="Profile name to run")
    parser.add_argument(
        "--list", action="store_true", help="List available profiles and exit"
    )
    args = parser.parse_args()

    profiles = load_profiles()

    if args.list or not args.profile:
        print("Available profiles:")
        for name, p in profiles.items():
            print(
                f"  {name:12s} rps={p.get('rps')} duration_s={p.get('duration_seconds')} "
                f"fail_ratio={(p.get('payments') or {}).get('fail_ratio', 0.0)}"
            )
        if not args.profile:
            sys.exit(0 if args.list else 2)

    if args.profile not in profiles:
        raise SystemExit(
            f"Unknown profile {args.profile!r}. "
            f"Available: {sorted(profiles)}"
        )

    start_http_server(METRICS_PORT)
    print(
        f"[loadgen] /metrics listening on :{METRICS_PORT}", flush=True
    )

    asyncio.run(run_profile(args.profile, profiles[args.profile]))


if __name__ == "__main__":
    main()

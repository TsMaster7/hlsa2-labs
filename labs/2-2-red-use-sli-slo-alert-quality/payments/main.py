"""Payments stub: deterministic-ish failure injector for the checkout lab.

State is held in process memory and seeded from env on startup. The load
generator can flip faults on and off mid-experiment by POSTing to
``/admin/inject``. This mirrors a realistic failure mode where the
downstream dependency degrades, not the service under test itself.

Env defaults (applied at startup):

  PAYMENTS_FAIL_RATIO         float in [0, 1]   default 0.0
  PAYMENTS_LATENCY_MS         int (ms)          default 30
  PAYMENTS_LATENCY_JITTER_MS  int (ms)          default 20

Runtime override:

  POST /admin/inject  {"fail_ratio": 0.05, "latency_ms": 30, "latency_jitter_ms": 20}

The stub's own /metrics is exposed so Prometheus can confirm it is alive.
The SLI under measurement is the checkout service, not this stub.
"""

from __future__ import annotations

import asyncio
import os
import random
from dataclasses import dataclass

from fastapi import FastAPI
from fastapi.responses import JSONResponse, PlainTextResponse, Response
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    Counter,
    Gauge,
    generate_latest,
)
from pydantic import BaseModel, Field


@dataclass
class InjectionState:
    fail_ratio: float
    latency_ms: int
    latency_jitter_ms: int


def _env_float(name: str, default: float) -> float:
    try:
        return float(os.environ.get(name, default))
    except (TypeError, ValueError):
        return default


def _env_int(name: str, default: int) -> int:
    try:
        return int(os.environ.get(name, default))
    except (TypeError, ValueError):
        return default


state = InjectionState(
    fail_ratio=max(0.0, min(1.0, _env_float("PAYMENTS_FAIL_RATIO", 0.0))),
    latency_ms=max(0, _env_int("PAYMENTS_LATENCY_MS", 30)),
    latency_jitter_ms=max(0, _env_int("PAYMENTS_LATENCY_JITTER_MS", 20)),
)

charges_total = Counter(
    "payments_charges_total",
    "Total charge requests handled by the payments stub.",
    labelnames=("outcome",),
)

inject_fail_ratio = Gauge(
    "payments_inject_fail_ratio",
    "Currently configured fault-injection ratio on the payments stub.",
)
inject_latency_ms = Gauge(
    "payments_inject_latency_ms",
    "Currently configured simulated latency on the payments stub (mean ms).",
)
inject_fail_ratio.set(state.fail_ratio)
inject_latency_ms.set(state.latency_ms)

app = FastAPI(title="payments-stub", version="1.0.0")


class InjectRequest(BaseModel):
    fail_ratio: float = Field(ge=0.0, le=1.0)
    latency_ms: int = Field(ge=0, default=30)
    latency_jitter_ms: int = Field(ge=0, default=20)


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok", "service": "payments"}


@app.get("/metrics")
async def metrics() -> Response:
    return PlainTextResponse(
        generate_latest(), media_type=CONTENT_TYPE_LATEST
    )


@app.post("/admin/inject")
async def admin_inject(req: InjectRequest) -> dict:
    state.fail_ratio = req.fail_ratio
    state.latency_ms = req.latency_ms
    state.latency_jitter_ms = req.latency_jitter_ms
    inject_fail_ratio.set(state.fail_ratio)
    inject_latency_ms.set(state.latency_ms)
    return {
        "status": "ok",
        "fail_ratio": state.fail_ratio,
        "latency_ms": state.latency_ms,
        "latency_jitter_ms": state.latency_jitter_ms,
    }


@app.get("/admin/inject")
async def admin_inject_get() -> dict:
    return {
        "fail_ratio": state.fail_ratio,
        "latency_ms": state.latency_ms,
        "latency_jitter_ms": state.latency_jitter_ms,
    }


@app.post("/charge")
async def charge() -> Response:
    delay_ms = state.latency_ms + random.uniform(
        -state.latency_jitter_ms, state.latency_jitter_ms
    )
    delay_ms = max(0.0, delay_ms)
    await asyncio.sleep(delay_ms / 1000.0)

    if random.random() < state.fail_ratio:
        charges_total.labels(outcome="error").inc()
        return JSONResponse(
            {"status": "error", "reason": "simulated_payments_failure"},
            status_code=500,
        )

    charges_total.labels(outcome="ok").inc()
    return JSONResponse(
        {"status": "ok", "amount_cents": 4999, "delay_ms": round(delay_ms, 2)}
    )


@app.get("/")
async def root() -> dict:
    return {
        "service": "payments-stub",
        "fail_ratio": state.fail_ratio,
        "latency_ms": state.latency_ms,
        "latency_jitter_ms": state.latency_jitter_ms,
    }

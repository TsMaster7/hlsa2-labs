"""Checkout service: deliberately fragile FastAPI app under SLO.

POST /checkout calls the payments stub. /metrics exposes RED instruments
with bounded-cardinality labels (route, method, status_class). The latency
histogram has a bucket boundary at exactly 0.3s so the latency SLI can be
computed precisely against a 300ms threshold without re-instrumenting.

Health and metrics endpoints are excluded from the RED counters by design:
they are not part of the user-facing SLI denominator.
"""

from __future__ import annotations

import os
import time
import uuid

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse, PlainTextResponse
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    Counter,
    Gauge,
    Histogram,
    generate_latest,
)

PAYMENTS_URL = os.environ.get("PAYMENTS_URL", "http://payments:8081/charge")
PAYMENTS_TIMEOUT_S = float(os.environ.get("PAYMENTS_TIMEOUT_S", "2.0"))
SERVICE_NAME = os.environ.get("SERVICE_NAME", "checkout")

# Histogram buckets are calibrated to a 300ms latency SLO threshold.
# The 0.3 boundary is mandatory: it lets the student write a precise
# threshold-style SLI as
#   sum(rate(http_request_duration_seconds_bucket{le="0.3", ...}[5m]))
#   / sum(rate(http_request_duration_seconds_count{...}[5m]))
LATENCY_BUCKETS = (0.05, 0.1, 0.2, 0.3, 0.5, 0.8, 1.0, 1.5, 2.0, 3.0, 5.0)

http_requests_total = Counter(
    "http_requests_total",
    "Total HTTP requests handled by the checkout service.",
    labelnames=("route", "method", "status_class"),
)

http_request_duration_seconds = Histogram(
    "http_request_duration_seconds",
    "HTTP request duration in seconds.",
    labelnames=("route", "method"),
    buckets=LATENCY_BUCKETS,
)

inflight_requests = Gauge(
    "inflight_requests",
    "Currently in-flight HTTP requests on the checkout service.",
)

app = FastAPI(title="checkout", version="1.0.0")


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


def _is_instrumented_route(path: str) -> bool:
    # /healthz and /metrics are not part of the user-facing SLI.
    return path not in ("/healthz", "/metrics", "/")


@app.middleware("http")
async def red_middleware(request: Request, call_next):
    path = request.url.path
    method = request.method

    if not _is_instrumented_route(path):
        return await call_next(request)

    inflight_requests.inc()
    start = time.perf_counter()
    response: Response
    try:
        response = await call_next(request)
        status_code = response.status_code
    except Exception:
        status_code = 500
        response = JSONResponse(
            {"error": "internal_error"}, status_code=500
        )
    finally:
        elapsed = time.perf_counter() - start
        inflight_requests.dec()
        # Use the route template via FastAPI route attr (e.g. "/checkout")
        # rather than the raw path, so cardinality stays bounded.
        route = request.scope.get("route")
        route_template = getattr(route, "path", path) if route else path
        http_request_duration_seconds.labels(
            route=route_template, method=method
        ).observe(elapsed)
        http_requests_total.labels(
            route=route_template,
            method=method,
            status_class=_status_class(status_code),
        ).inc()
    return response


@app.get("/healthz")
async def healthz() -> dict:
    return {"status": "ok", "service": SERVICE_NAME}


@app.get("/metrics")
async def metrics() -> Response:
    return PlainTextResponse(
        generate_latest(), media_type=CONTENT_TYPE_LATEST
    )


@app.post("/checkout")
async def checkout(request: Request) -> Response:
    request_id = request.headers.get("x-request-id") or str(uuid.uuid4())
    try:
        async with httpx.AsyncClient(timeout=PAYMENTS_TIMEOUT_S) as client:
            r = await client.post(
                PAYMENTS_URL,
                json={"request_id": request_id, "amount_cents": 4999},
                headers={"x-request-id": request_id},
            )
    except httpx.TimeoutException:
        return JSONResponse(
            {"error": "payments_timeout", "request_id": request_id},
            status_code=504,
        )
    except httpx.HTTPError:
        return JSONResponse(
            {"error": "payments_unreachable", "request_id": request_id},
            status_code=502,
        )

    if r.status_code >= 500:
        return JSONResponse(
            {"error": "payments_failed", "request_id": request_id},
            status_code=502,
        )
    if r.status_code >= 400:
        return JSONResponse(
            {"error": "payments_rejected", "request_id": request_id},
            status_code=r.status_code,
        )

    return JSONResponse(
        {
            "status": "ok",
            "request_id": request_id,
            "payments": r.json(),
        }
    )


@app.get("/")
async def root() -> dict:
    return {"service": SERVICE_NAME, "endpoints": ["/checkout", "/healthz", "/metrics"]}

# SLO design for `POST /checkout`

> **Deliverable** — corresponds to homework deliverable **id=430**:
> "SLO design document". Replace every `TODO` block before submitting.

## 1. Service under measurement

- Service: `checkout` (FastAPI, see `checkout/main.py`).
- Endpoint under SLO: `POST /checkout`.
- Measurement point: the `checkout` service's own `/metrics`. The
  alternative measurement points considered are listed below in
  Section 5.

## 2. Availability SLI

| Field           | Value |
|-----------------|-------|
| Numerator (PromQL)   | `sum(rate(http_requests_total{service="checkout",route="/checkout",status_class=~"2..\|3.."}[<window>]))` |
| Denominator (PromQL) | `sum(rate(http_requests_total{service="checkout",route="/checkout"}[<window>]))` |
| Excluded events      | `/healthz`, `/metrics` (excluded by the service itself, not the SLI). 4xx is **TODO** — keep or drop? Justify. |
| Measurement point    | Server-side, on the checkout service. |
| Windows used         | 5m, 30m, 1h, 6h (driven by the burn-rate alert math). |

**SLO target:** `TODO` — choose 1–2 nines below your observed
steady-state from the baseline. Steady-state from `artifacts/01-baseline/`
is `TODO` (cite the number).

**Rejected alternatives:**

- `TODO` — write at least two SLI alternatives you considered and
  rejected, and *why*. Examples: window-based SLI; client-side SLI; a
  composite SLI mixing availability and latency.

## 3. Latency SLI

| Field           | Value |
|-----------------|-------|
| Numerator (PromQL)   | `TODO` — use the histogram bucket count under the SLO threshold. The histogram has an explicit bucket boundary at `0.3s` so a threshold-style SLI is exact, not approximate. |
| Denominator (PromQL) | `TODO` — the total observed count. |
| Excluded events      | `/healthz`, `/metrics`. 4xx is `TODO` — keep or drop? |
| Measurement point    | Server-side, on the checkout service. |
| Windows used         | 5m, 30m, 1h, 6h. |
| Threshold            | `TODO` — e.g. 300 ms. Pick from your baseline. |

**SLO target:** `TODO`. Show the baseline number that justifies it.

**Why threshold-style, not percentile-style?**

`TODO` — explain why the SLI is `requests_under_T_ms / total_requests`
rather than alerting on `histogram_quantile(0.99, ...)`. Reference
heavy-tail tenants and bucket aggregation behavior.

**Why the bucket boundary at the SLO threshold matters:**

`TODO` — what would change if the histogram had no bucket exactly at
0.3s and you tried to compute the SLI at 300 ms?

## 4. Error budget

- 30-day error budget for availability: `TODO` — `(1 - SLO_target) * 30d` of error events.
- 30-day error budget for latency: `TODO`.
- Burn-rate definition: `(1 - SLI) / (1 - SLO)`.

## 5. Rejected SLI alternatives

`TODO` list. At minimum, address:

1. Why not measure on the client / load balancer instead of the service?
2. Why not a window-based SLI?
3. Why not a single composite SLI for both availability and latency?

## 6. Sources

- Baseline data dump: `artifacts/01-baseline/` (committed).
- Recording rules: `prometheus/rules/slo.yml`.
- Multi-burn-rate alerts: same file, Layer C.

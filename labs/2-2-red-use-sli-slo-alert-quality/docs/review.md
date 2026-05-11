# Architecture review — `POST /checkout` SLO and multi-burn-rate alerts

> **Deliverable** — corresponds to homework deliverable **id=433**:
> "Architecture review and error-budget policy". Minimum length: ≥ 1
> page of dense prose plus the tables. Replace every `TODO`.

## 1. SLI design choices and rejected alternatives

`TODO` — summarise the two SLIs from `docs/slo.md`. For each, list the
alternatives you considered and the reason for rejecting each. Be
specific: not "client-side felt expensive" but "client-side adds N high-
cardinality labels per session, which we measured at X series".

## 2. Burn-rate math: why 14.4× and 6×

`TODO` — derive both numbers. Show the math:

- A 30-day error budget allows `(1 - SLO) * 30d` of bad events.
- A burn rate of `B` consumes the budget in `30d / B`.
- 14.4× over 1h consumes `2%` of the 30-day budget — the page threshold.
- 6× over 6h consumes `5%` — the ticket threshold.

Then explain why the **AND of two windows** is non-negotiable: the long
window confirms the issue is real, the short window confirms it is
still happening.

## 3. Three-experiment results

`TODO` — fill the table from `artifacts/02-experiments/`.

| Experiment             | Profile      | Duration | Errors injected | Fast-burn fired? | Slow-burn fired? | TTD (s) | TTR (s) |
|------------------------|-------------|---------:|----------------:|:----------------:|:----------------:|--------:|--------:|
| 1. Steady-state        | nominal     |    30 min |  0%             |       no         |       no         |    n/a  |    n/a  |
| 2. Fast-burn outage    | fast-burn   |    12 min |  5%             |       yes        |       no         |  `TODO` |  `TODO` |
| 3. Slow-burn degradation | slow-burn |    90 min |  0.7%           |       no         |       yes        |  `TODO` |  `TODO` |

Glossary:
- **TTD** (time-to-detect): seconds from injection start to alert firing.
- **TTR** (time-to-reset): seconds from injection stop to alert clearing.

## 4. False-positive analysis

`TODO`. Did either alert fire during the baseline experiment? If yes,
explain the root cause and the fix. If no, explain why the alert
geometry guarantees this — e.g. "the dual-window AND with `for: 2m`
suppresses single-scrape glitches and brief noise spikes below 2 minutes".

## 5. Error-budget policy

`TODO`. Write a real policy, not platitudes. At minimum cover:

- **Freeze threshold**: at what budget level (e.g. <25% remaining) do
  we stop shipping non-essential changes?
- **Override approver**: who can sign off on shipping during a freeze,
  and what evidence do they require?
- **Exit criteria**: what budget level + what time window (e.g. ≥50%
  remaining for 7 days) lifts the freeze?
- **Special cases**: launch weeks, security fixes, regulatory deadlines.

## 6. Cardinality audit

`TODO`. List every label you ship to Prometheus and every label you
considered but rejected. Include actual values from
`/api/v1/status/tsdb` or `count by (__name__)({__name__=~"http_.*"})`.

| Metric                          | Label          | Distinct values | Source of bound          | Decision |
|---------------------------------|----------------|-----------------|--------------------------|----------|
| `http_requests_total`           | `route`        |  `TODO`         | Router templates only    | keep     |
| `http_requests_total`           | `method`       |  `TODO`         | HTTP spec (~7 verbs)     | keep     |
| `http_requests_total`           | `status_class` |       4         | Hard-coded enum          | keep     |
| `http_request_duration_seconds` | `route`, `method`, `le` | `TODO` × 11 buckets | as above + buckets | keep |
| _rejected_                      | `user_id`      | unbounded       | none                     | rejected — would explode TSDB |
| _rejected_                      | `request_id`   | unbounded       | none                     | rejected — belongs in logs |
| _rejected_                      | `raw_url_path` | unbounded       | none                     | rejected — instantiated paths kill cardinality |

## 7. 10× growth analysis

`TODO`. If traffic grew from 50 RPS to 500 RPS tomorrow, what saturates
first?

- Worker pool / event loop on the checkout container?
- Outbound HTTP socket pool to the payments stub?
- Prometheus TSDB ingestion rate?
- Alertmanager fan-out?
- Grafana query latency on the burn-rate panels?

For each, cite the resource you would measure and the design change you
would make in response (e.g. add an upstream queue, switch to a
push-based metrics pipeline, add per-route SLOs to localise blast radius).

## 8. What I would do differently next iteration

`TODO`. One paragraph. Be honest.

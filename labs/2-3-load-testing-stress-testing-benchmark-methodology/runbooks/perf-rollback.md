# Runbook — Performance Rollback Trigger

This runbook defines the auto-rollback trigger declared in Section 7
of `docs/review.md`. Edit it to match the threshold you commit there.

## Trigger

Auto-revert the candidate revision when:

> TODO: paste the exact PromQL and threshold from `docs/review.md`.
>
> Example:
> ```
> histogram_quantile(0.99,
>   sum by (le) (
>     rate(http_request_duration_seconds_bucket{
>       service="sut", endpoint="/medium"
>     }[5m])
>   )
> ) > 0.200
> ```
> for 5 consecutive minutes after rollout.

## Why this metric

TODO — one paragraph. Reference the bottleneck analysis in Section 3
of `docs/review.md`. The trigger should be the metric that moved when
the regression you are guarding against occurred, not a generic SLO
indicator.

## Operator steps when the trigger fires

1. Confirm the alert in Grafana (`use-red-overview` dashboard, panel
   "Latency p99 by endpoint").
2. Check Layer 4 first: did `sut:pgxpool:acquire_wait:p99` move? If
   yes, the candidate revision regressed pool sizing.
3. Run `kubectl rollout undo` (or your deployment system's equivalent)
   to revert to the previous revision.
4. Re-run `make analyze-regression` against the post-revert run plus
   the most recent baseline; confirm the p99 returned to within 2σ.
5. Open a postmortem ticket with the candidate diff, the trigger
   timestamp, and the saturation panel screenshot.

## Why this is not a generic SLO alert

A generic "p99 > X" alert pages on every cause (downstream slow,
network spike, GC storm). This trigger is **specific to the candidate
deploy**: it should fire only on the metric that the candidate
optimization touches. That makes it auto-revert-safe.

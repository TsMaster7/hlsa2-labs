# Runbook: Synchronous chain availability incident

**Trigger:** `lab32:gateway_p99_seconds` is above its SLO and at least
one `lab32:hop_success_rate{hop=...}` is below 99.5%.

## 1. Confirm scope

1. Open the Sync/Async Overview dashboard (Grafana ->
   `sync-async-overview`). Look at the "Sync chain" row.
2. Confirm which hop's success-rate panel is below the rest. The hop
   with the lowest rate is the proximal cause.
3. Confirm `lab32_gateway_breaker_open{hop=<slow-hop>}` - if it is 1,
   the breaker is already shielding the caller; if 0, the caller is
   still queueing on the slow hop and the blast radius is wider.

## 2. Decide between mitigate-here vs page-the-owner

| Symptom                                       | Action                                                            |
|-----------------------------------------------|-------------------------------------------------------------------|
| Single hop is hot; others healthy             | Page the hop's on-call. Do NOT scale the gateway.                 |
| Multiple hops degrading together              | Suspect a shared dependency (DB, cache, network). Escalate.       |
| Breaker stuck open beyond cooldown            | Confirm the hop is actually healthy with a manual probe (`curl`). |
| Caller p99 tracking slow hop's tail exactly   | Topic's "tail amplification": expect mitigation to take O(minutes), not seconds. |

## 3. Tactical mitigations

- Turn the breaker on if it is not already:
  `CIRCUIT_BREAKER=on docker compose up -d --force-recreate gateway`.
  This will not save the requests that genuinely need the open hop -
  it just stops the caller's connection pool from saturating on slow
  timeouts.
- If the slow hop has an `/admin/inject` (this lab only - in production
  this would be a fault-injection or feature-flag endpoint), confirm
  the injection is not yours by mistake.
- Shed traffic at the gateway if the upstream caller can tolerate 503s
  better than queueing.

## 4. Things to NOT do

- Do not retry inside the gateway without a budget - retries amplify
  the load on the slow hop and you re-create the "thundering recovery"
  failure mode the topic warns about.
- Do not change the hop's container resource limits during the
  incident. That changes the fingerprint your post-mortem will use.

## 5. Post-incident

- Capture: which hop, peak `lab32:gateway_p99_seconds`, peak observed
  success-rate dip on `lab32:hop_success_rate`, breaker open duration.
- Compare measured E2E availability against the product of per-hop
  availabilities during the incident - they should match. If they
  don't, you have a second cause we didn't notice.
- Update the SLO error budget burn and decide whether the topic's
  "convert this hop's side-effect to an event" architecture change is
  justified.

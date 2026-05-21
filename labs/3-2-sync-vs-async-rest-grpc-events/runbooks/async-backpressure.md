# Runbook: Async path is falling behind

**Trigger:** `lab32_consumer_queue_depth` is climbing for more than
two minutes AND `lab32:consumer_event_to_effect_p99_seconds` is rising
together, while `lab32:consumer_handler_p99_seconds` stays flat.

This is the canonical "the backlog, not the handler, is the problem"
signal from step 5 of the topic.

## 1. Confirm

1. Open the Async row of the Sync/Async Overview dashboard.
2. Confirm that producer emit rate is above the consumer's handled
   rate (look at `lab32:producer_emitted_msgs_per_s` vs
   `lab32:consumer_throughput_msgs_per_s`).
3. Note whether `lab32_consumer_shed_signal` is 1. If it is, flow
   control is already engaged and the system is shedding load.

## 2. Decide between let-bp-do-its-job vs incident

| Signal                                        | Action                                                    |
|-----------------------------------------------|-----------------------------------------------------------|
| Shed signal = 1, queue depth bounded          | Working as designed. Open a ticket against producer.      |
| Shed signal = 0, depth climbing               | Flow control is off. Turn it on: `curl -X POST -H 'content-type: application/json' -d '{"on":true}' http://consumer:8095/flow`. |
| Postgres connections saturated                | Downstream is the bottleneck. Throttle replay if a replay is in flight: `REPLAY_RATE_LIMIT=500`. |
| `lab32_consumer_dlq_total` climbing           | Suspect poison messages. Pause the producer, drain the DLQ, root-cause. |

## 3. Tactical mitigations

- Enable flow control if it is not already: bounded queue, drop-oldest,
  producer 429.
- If the consumer has been running with `FLOW_CONTROL=off` and the
  queue is approaching `ASYNC_MAX_QUEUE_GUARD`, restart the consumer
  with `FLOW_CONTROL=on` BEFORE the guard trips - the guard is a
  last-resort.
- If a replay is in flight and Postgres is the bottleneck, lower
  `REPLAY_RATE_LIMIT`. The trade is: longer replay duration for less
  downstream impact.

## 4. Things to NOT do

- Do not delete the consumer's queue to "speed things up". You lose
  unacked events.
- Do not retry from the DLQ blindly. The DLQ exists because something
  failed; retrying without a budget recreates the failure.

## 5. Post-incident

- Capture: peak `lab32_consumer_queue_depth`, peak
  `lab32:consumer_event_to_effect_p99_seconds`, drop count from
  `lab32_consumer_dropped_total`, DLQ depth.
- If shed signal stayed on for >5 minutes, the producer's load is
  structurally too high - that is a capacity-planning ticket, not an
  on-call issue.

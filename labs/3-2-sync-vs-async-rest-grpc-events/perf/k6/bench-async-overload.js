// Step 5 driver. Tells the producer to start a stream well above the
// consumer's sustainable rate, then polls /state for queue depth +
// shed counters. k6 itself doesn't generate the events - the
// producer does, on the broker side - but k6 is the harness so the
// run lives under perf/results alongside everything else.

import http from 'k6/http';
import { sleep } from 'k6';

const PRODUCER_URL = __ENV.PRODUCER_URL || 'http://localhost:8094';
const CONSUMER_URL = __ENV.CONSUMER_URL || 'http://localhost:8095';
const PRODUCER_RATE = Number(__ENV.PRODUCER_RATE || 5000);
const CONSUMER_RATE = Number(__ENV.CONSUMER_RATE || 1500);
const DURATION_S = Number(__ENV.DURATION_S || 300);
const FLOW_CONTROL = __ENV.FLOW_CONTROL || 'off';
const LABEL = __ENV.LABEL || 'unlabeled';

export const options = {
  scenarios: {
    drive: {
      executor: 'shared-iterations',
      iterations: 1,
      vus: 1,
      maxDuration: `${DURATION_S + 60}s`,
      exec: 'drive',
    },
  },
};

export function drive() {
  // Flip flow control on the consumer
  http.post(`${CONSUMER_URL}/flow`, JSON.stringify({ on: FLOW_CONTROL === 'on' }), {
    headers: { 'content-type': 'application/json' },
  });
  // Start the stream
  http.post(`${PRODUCER_URL}/start`, JSON.stringify({
    rate: PRODUCER_RATE,
    duration_s: DURATION_S,
    window_s: 0,
    source: `overload-${LABEL}`,
  }), {
    headers: { 'content-type': 'application/json' },
    tags: { phase: 'control', label: LABEL },
  });

  const endAt = new Date().getTime() + DURATION_S * 1000;
  while (new Date().getTime() < endAt) {
    // Sample queue + shed state every second so the harness has a
    // visible CSV-able trail of how the run unfolded.
    const cs = http.get(`${CONSUMER_URL}/state`, { tags: { phase: 'sample', label: LABEL } });
    if (cs.status === 200) {
      const body = JSON.parse(cs.body);
      console.log(`[${LABEL}] queue=${body.queue_depth} shed=${body.shed} aborted=${body.aborted}`);
      if (body.aborted) {
        console.warn(`[${LABEL}] consumer aborted at queue_depth=${body.queue_depth}; stopping the producer.`);
        http.post(`${PRODUCER_URL}/stop`);
        return;
      }
    }
    sleep(1);
  }
  // Final stop in case duration ran out before producer naturally finished
  http.post(`${PRODUCER_URL}/stop`);
}

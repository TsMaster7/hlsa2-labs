// Closed-loop generator - intentionally "wrong" for the omission demo.
//
// Why this script exists:
//
//   The whole point of Step 4 is to PROVE empirically that a closed-loop
//   generator hides tail latency when the SUT stalls. We pin VUs low
//   (default 1) so each VU is blocked while the stall is in progress,
//   which means the generator emits no samples for the duration of the
//   stall. The recorded p99 stays small. The same SUT, hit by the
//   open-loop baseline.js, shows the true tail because requests keep
//   arriving on schedule and getting queued.
//
// Use this script only against a SUT whose downstream has the stall
// switch turned ON (see scripts/run-experiment.sh closed-loop, which
// flips it for you).

import http from 'k6/http';
import { check } from 'k6';
import { loadWorkload, pickEndpoint } from './lib/workload.js';

const wl = loadWorkload();

const SUT_URL = __ENV.SUT_URL || 'http://localhost:8080';
const VUS = Number(__ENV.K6_VUS || 1);
const DURATION_S = Number(__ENV.DURATION_S || wl.measured_seconds);

export const options = {
  scenarios: {
    closed_loop: {
      executor: 'constant-vus',
      vus: VUS,
      duration: `${DURATION_S}s`,
      exec: 'fire',
      tags: { phase: 'measured', generator: 'closed-loop' },
    },
  },
  thresholds: {
    // Intentionally no thresholds: the recorded p99 here is the
    // omission-affected number we are demonstrating.
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

export function fire() {
  const ep = pickEndpoint(wl);
  const url = `${SUT_URL}${ep.path}`;
  const params = { tags: { endpoint: ep.key, phase: 'measured' } };
  const resp = http.request(ep.method, url, null, params);
  check(resp, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  }, { endpoint: ep.key });
}

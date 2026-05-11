// Soak test - 4 hours at 50% of saturation rate.
//
// SOAK_RPS defaults to half of workload.target_rps. Override with
// SOAK_RPS=N to match the half-of-knee number you observed in your
// own ramp test.
//
// No thresholds: the soak test is observational. The deliverable is
// the four-hour Grafana panel set, not a pass/fail at the end.

import http from 'k6/http';
import { check } from 'k6';
import { loadWorkload, pickEndpoint } from './lib/workload.js';

const wl = loadWorkload();

const SUT_URL = __ENV.SUT_URL || 'http://localhost:8080';
const SOAK_RPS = Number(__ENV.SOAK_RPS || Math.max(1, Math.floor(wl.target_rps / 2)));
const DURATION_S = Number(__ENV.DURATION_S || wl.soak_seconds);
const PRE_VUS = Number(__ENV.PRE_VUS || 50);
const MAX_VUS = Number(__ENV.MAX_VUS || 500);

export const options = {
  scenarios: {
    soak: {
      executor: 'constant-arrival-rate',
      rate: SOAK_RPS,
      timeUnit: '1s',
      duration: `${DURATION_S}s`,
      preAllocatedVUs: PRE_VUS,
      maxVUs: MAX_VUS,
      exec: 'fire',
      tags: { phase: 'soak' },
    },
  },
  thresholds: {},
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

export function fire() {
  const ep = pickEndpoint(wl);
  const url = `${SUT_URL}${ep.path}`;
  const params = { tags: { endpoint: ep.key, phase: 'soak' } };
  const resp = http.request(ep.method, url, null, params);
  check(resp, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  }, { endpoint: ep.key });
}

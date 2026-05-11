// Open-loop baseline.
//
// Uses constant-arrival-rate so the offered load is not coupled to
// in-flight latency. Two scenarios:
//
//   warmup   - 60s, separate tag so its samples can be excluded from
//              the per-endpoint percentile assertions.
//   measured - 5m, the window the SLO thresholds apply to.
//
// Per-endpoint thresholds match the homework's Step 3 example. The
// endpoint label is set per-request via the `tags` argument so the
// http_req_duration metric can be sliced by route.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { loadWorkload, pickEndpoint } from './lib/workload.js';

const wl = loadWorkload();

const SUT_URL = __ENV.SUT_URL || 'http://localhost:8080';
const TARGET_RPS = Number(__ENV.TARGET_RPS || wl.target_rps);
const WARMUP_S = Number(__ENV.WARMUP_S || wl.warmup_seconds);
const MEASURED_S = Number(__ENV.MEASURED_S || wl.measured_seconds);
const PRE_VUS = Number(__ENV.PRE_VUS || 50);
const MAX_VUS = Number(__ENV.MAX_VUS || 500);

export const options = {
  scenarios: {
    warmup: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: `${WARMUP_S}s`,
      preAllocatedVUs: PRE_VUS,
      maxVUs: MAX_VUS,
      exec: 'fire',
      tags: { phase: 'warmup' },
      env: { PHASE: 'warmup' },
    },
    measured: {
      executor: 'constant-arrival-rate',
      rate: TARGET_RPS,
      timeUnit: '1s',
      duration: `${MEASURED_S}s`,
      startTime: `${WARMUP_S}s`,
      preAllocatedVUs: PRE_VUS,
      maxVUs: MAX_VUS,
      exec: 'fire',
      tags: { phase: 'measured' },
      env: { PHASE: 'measured' },
    },
  },
  thresholds: {
    'http_req_duration{phase:measured,endpoint:fast}':   ['p(99)<50'],
    'http_req_duration{phase:measured,endpoint:medium}': ['p(99)<150'],
    'http_req_duration{phase:measured,endpoint:slow}':   ['p(99)<500'],
    'http_req_failed{phase:measured}':                   ['rate<0.001'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
  // No discardResponseBodies so the slow path's payload size is realistic.
};

export function fire() {
  const ep = pickEndpoint(wl);
  const url = `${SUT_URL}${ep.path}`;
  const params = { tags: { endpoint: ep.key, phase: __ENV.PHASE || 'measured' } };
  let resp;
  if (ep.method === 'GET') {
    resp = http.get(url, params);
  } else {
    resp = http.request(ep.method, url, null, params);
  }
  check(resp, {
    'status is 2xx': (r) => r.status >= 200 && r.status < 300,
  }, { endpoint: ep.key });

  // No think time in the baseline - the constant-arrival-rate executor
  // already controls inter-arrival pacing.
}

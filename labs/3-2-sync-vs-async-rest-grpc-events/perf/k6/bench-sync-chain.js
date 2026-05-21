// Step 4 driver. Drives the gateway's /sync-chain at a steady
// arrival rate and tags each request with the LABEL the harness sets,
// so the analyzer can compare 'healthy' vs 'faulted' (vs
// 'faulted-breaker') runs.

import http from 'k6/http';
import { check } from 'k6';
import { loadWorkload, pickKey } from './lib/workload.js';

const wl = loadWorkload();

const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://localhost:8090';
const TARGET_RPS = Number(__ENV.TARGET_RPS || 100);
const WARMUP_S = Number(__ENV.WARMUP_S || 20);
const MEASURED_S = Number(__ENV.MEASURED_S || 120);
const PRE_VUS = Number(__ENV.PRE_VUS || 50);
const MAX_VUS = Number(__ENV.MAX_VUS || 500);
const LABEL = __ENV.LABEL || 'unlabeled';

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
      tags: { chain_label: LABEL, phase: 'warmup' },
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
      tags: { chain_label: LABEL, phase: 'measured' },
      env: { PHASE: 'measured' },
    },
  },
  thresholds: {
    'http_req_failed{phase:measured}': ['rate<0.5'], // intentionally loose; the analyzer reports the real number
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

export function fire() {
  const key = pickKey(wl);
  const url = `${GATEWAY_URL}/sync-chain?key=${key}`;
  const resp = http.get(url, {
    tags: { phase: __ENV.PHASE || 'measured', chain_label: LABEL },
  });
  check(resp, {
    'status 200': (r) => r.status === 200,
  });
}

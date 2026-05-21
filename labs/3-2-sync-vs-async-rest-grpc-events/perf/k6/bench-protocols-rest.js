// REST/JSON `/lookup` benchmark for step 3 of topic 245.
//
// Constant-arrival-rate so the offered load is decoupled from
// in-flight latency. Warmup + measured scenarios so the percentile
// thresholds only apply to the measured window.

import http from 'k6/http';
import { check } from 'k6';
import { loadWorkload, pickSize, pickKey } from './lib/workload.js';

const wl = loadWorkload();

const REST_URL = __ENV.REST_URL || 'http://localhost:8080';
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
      tags: { protocol: 'rest', phase: 'warmup' },
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
      tags: { protocol: 'rest', phase: 'measured' },
      env: { PHASE: 'measured' },
    },
  },
  thresholds: {
    'http_req_duration{phase:measured}': ['p(99)<2000'], // generous so the bench never fails on slow boxes; analyzer enforces tightness
    'http_req_failed{phase:measured}':   ['rate<0.01'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

export function fire() {
  const key = pickKey(wl);
  const size = pickSize(wl);
  const url = `${REST_URL}/lookup?key=${key}&size=${size}`;
  const params = {
    tags: { protocol: 'rest', size, phase: __ENV.PHASE || 'measured' },
    headers: { 'accept': 'application/json' },
  };
  const resp = http.get(url, params);
  check(resp, {
    'status 2xx': (r) => r.status >= 200 && r.status < 300,
  });
}

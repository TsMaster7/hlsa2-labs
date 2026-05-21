// gRPC/Protobuf `/lookup` benchmark for step 3 of topic 245.
//
// Same arrival profile and payload distribution as
// bench-protocols-rest.js so the only delta between the two is the
// wire format + transport.

import grpc from 'k6/net/grpc';
import { check } from 'k6';
import { loadWorkload, pickSize, pickKey } from './lib/workload.js';

const wl = loadWorkload();

const GRPC_ADDR = __ENV.GRPC_ADDR || 'localhost:9000';
const TARGET_RPS = Number(__ENV.TARGET_RPS || wl.target_rps);
const WARMUP_S = Number(__ENV.WARMUP_S || wl.warmup_seconds);
const MEASURED_S = Number(__ENV.MEASURED_S || wl.measured_seconds);
const PRE_VUS = Number(__ENV.PRE_VUS || 50);
const MAX_VUS = Number(__ENV.MAX_VUS || 500);

const client = new grpc.Client();
client.load(['../../proto'], 'lookup.proto');

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
      tags: { protocol: 'grpc', phase: 'warmup' },
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
      tags: { protocol: 'grpc', phase: 'measured' },
      env: { PHASE: 'measured' },
    },
  },
  thresholds: {
    'grpc_req_duration{phase:measured}': ['p(99)<2000'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

export function fire() {
  if (!client.__connected) {
    client.connect(GRPC_ADDR, { plaintext: true });
    client.__connected = true;
  }
  const key = pickKey(wl);
  const size = pickSize(wl);
  const params = { tags: { protocol: 'grpc', size, phase: __ENV.PHASE || 'measured' } };
  const resp = client.invoke('hlsa2.lab32.Lookup/Lookup', { key, payload_size: size }, params);
  check(resp, {
    'status OK': (r) => r && r.status === grpc.StatusOK,
  });
}

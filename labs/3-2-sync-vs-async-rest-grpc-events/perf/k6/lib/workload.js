// Workload helpers shared by every k6 script in this lab.
//
// loadWorkload() reads perf/workload.json once in the init context
// (k6's open() is init-only). pickSize() and pickKey() are called
// per-iteration; they read from the loaded workload, never re-open
// the file.

const RAW = open('../../workload.json');

export function loadWorkload() {
  const wl = JSON.parse(RAW);
  if (!wl.target_rps) wl.target_rps = 200;
  if (!wl.warmup_seconds) wl.warmup_seconds = 30;
  if (!wl.measured_seconds) wl.measured_seconds = 180;
  if (!wl.payload_size_distribution) wl.payload_size_distribution = { small: 0.9, large: 0.1 };
  if (!wl.key_distribution) wl.key_distribution = { kind: 'zipf', skew: 1.1, n_keys: 50000 };
  return wl;
}

// pickSize: PAYLOAD env override wins, else draw from the configured
// distribution. The topic guide's two regimes (default vs PAYLOAD=large)
// both go through this.
export function pickSize(wl) {
  const forced = __ENV.PAYLOAD;
  if (forced === 'small' || forced === 'large') {
    return forced;
  }
  const pLarge = (wl.payload_size_distribution && wl.payload_size_distribution.large) || 0.1;
  return Math.random() < pLarge ? 'large' : 'small';
}

// pickKey: sqrt-skewed lookup id over n_keys.
export function pickKey(wl) {
  const n = (wl.key_distribution && wl.key_distribution.n_keys) || 50000;
  const u = Math.random();
  const id = Math.floor(u * u * n) + 1;
  return `k-${id}`;
}

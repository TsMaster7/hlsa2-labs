// Workload helpers shared between baseline.js, closed-loop.js and soak.js.
//
// loadWorkload() reads perf/workload.json at script init time using
// k6's open() (which runs in the init context, not per-VU). pickEndpoint()
// is called per-iteration and returns one of the configured endpoints
// according to its weight; weights do not need to sum to 100, they are
// normalised at load time.

const RAW = open('../../workload.json');

export function loadWorkload() {
  const wl = JSON.parse(RAW);

  // Normalise weights to a cumulative table for fast pickEndpoint().
  const items = [];
  let total = 0;
  for (const [key, ep] of Object.entries(wl.endpoints)) {
    if (ep.weight <= 0) continue;
    total += ep.weight;
    items.push({
      key,
      cum: total,
      path: ep.path,
      method: ep.method,
      threshold_p99_ms: ep.threshold_p99_ms,
    });
  }
  if (items.length === 0) {
    throw new Error('workload.json has no endpoints with weight > 0');
  }
  wl._cumulative = items;
  wl._cumulative_total = total;
  return wl;
}

export function pickEndpoint(wl) {
  const r = Math.random() * wl._cumulative_total;
  for (const it of wl._cumulative) {
    if (r < it.cum) return it;
  }
  return wl._cumulative[wl._cumulative.length - 1];
}

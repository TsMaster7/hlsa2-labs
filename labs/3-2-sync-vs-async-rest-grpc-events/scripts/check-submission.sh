#!/usr/bin/env bash
# check-submission.sh - verify every backtick-quoted filename in
# docs/review.md exists in the repo. Mirrors the topic-guide step 9
# expectation that supports the "every quantitative claim cites an
# artifact" rule.

set -uo pipefail

LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${LAB_ROOT}"

REVIEW="docs/review.md"
if [[ ! -f "${REVIEW}" ]]; then
  echo "FAIL: ${REVIEW} does not exist. Copy docs/review.template.md to docs/review.md and fill it in."
  exit 1
fi

# Extract every backtick-quoted token that looks like a path (contains
# '/' or ends with '.json' / '.png' / '.md' / '.txt' / '.csv').
MISSING=0
TOTAL=0

# We use grep -oE with PCRE-ish patterns supported by GNU/BSD grep.
mapfile -t CANDIDATES < <(
  grep -oE '`[A-Za-z0-9_./-]+`' "${REVIEW}" \
    | sed 's/^`//; s/`$//' \
    | sort -u \
    | awk '/[\/]|\.json$|\.png$|\.md$|\.txt$|\.csv$|\.jpg$/'
)

for raw in "${CANDIDATES[@]}"; do
  TOTAL=$((TOTAL + 1))
  # If the path doesn't exist literally, also try a glob ("perf/results/rest/run1/summary.json" etc).
  if [[ -e "${raw}" ]]; then
    continue
  fi
  # Try as a glob fragment - useful when the review mentions
  # "perf/results/rest/<RUN>/summary.json" with literal angle brackets.
  if compgen -G "${raw}" >/dev/null; then
    continue
  fi
  echo "MISSING: ${raw}"
  MISSING=$((MISSING + 1))
done

echo
if [[ "${MISSING}" -gt 0 ]]; then
  echo "FAIL: ${MISSING} / ${TOTAL} cited artefacts not found under the lab."
  exit 1
fi

echo "OK: ${TOTAL} cited artefacts all exist."

# Word count for the review (~1,500 - ~2,000 words).
WORDS="$(wc -w < "${REVIEW}" | tr -d '[:space:]')"
echo "review.md word count: ${WORDS}"
if [[ "${WORDS}" -lt 1200 ]]; then
  echo "WARN: review is under 1,200 words. The rubric expects ~1,500-2,000."
fi
if [[ "${WORDS}" -gt 2500 ]]; then
  echo "WARN: review is over 2,500 words. Tighten it - the rubric expects ~1,500-2,000."
fi

# Make sure no TODO markers remain.
if grep -q 'TODO' "${REVIEW}"; then
  echo "WARN: docs/review.md still contains TODO markers."
fi

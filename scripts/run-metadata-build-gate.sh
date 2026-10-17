#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${JH_METADATA_BASELINE_REPOSITORY:?exact pre-adaptive plugin and SDK repository is required}"
: "${JH_METADATA_BASELINE_VERSION:?pre-adaptive version is required}"
: "${JH_METADATA_CANDIDATE_REPOSITORY:?exact candidate repository is required}"
: "${JANKHUNTER_VERSION:?candidate version is required}"
: "${JH_METADATA_GRADLE:?consumer Gradle distribution is required}"
OUTPUT="${1:?new evidence directory is required}"
PAIRS="${METADATA_BENCHMARK_PAIRS:-10}"
if [[ "$PAIRS" -lt 10 ]]; then
  echo "Release performance gate requires at least ten pairs; use measure directly for smoke trials" >&2
  exit 2
fi
python3 "$ROOT/scripts/metadata-build-benchmark.py" plan \
  --baseline-repository "$JH_METADATA_BASELINE_REPOSITORY" \
  --candidate-repository "$JH_METADATA_CANDIDATE_REPOSITORY" \
  --baseline-version "$JH_METADATA_BASELINE_VERSION" --candidate-version "$JANKHUNTER_VERSION" \
  --gradle "$JH_METADATA_GRADLE" --output "$OUTPUT" \
  --classes "${METADATA_BENCHMARK_CLASSES:-100000}" --modules "${METADATA_BENCHMARK_MODULES:-20}"
python3 "$ROOT/scripts/metadata-build-benchmark.py" measure \
  --manifest "$OUTPUT/manifest.json" --output "$OUTPUT/results" --pairs "$PAIRS"
cp "$OUTPUT/results/results.json" "$OUTPUT/sc003-results.json"

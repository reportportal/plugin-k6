#!/bin/sh
# Builds the k6 image with the ReportPortal output and runs one of the example scripts in it.
#
#   cp examples/reportportal.env.example examples/reportportal.env   # then edit it
#   examples/run-docker.sh                                           # runs script.js
#   examples/run-docker.sh failure-details.js
#
# IMAGE, ENV_FILE and PLUGIN can be overridden. PLUGIN=github.com/reportportal/plugin-k6@vX.Y.Z
# builds a release tag instead of this checkout.
# --network host is for Linux. It lets the container reach a mock or a server on localhost.
set -eu

cd "$(dirname "$0")/.."

IMAGE=${IMAGE:-perf-k6}
ENV_FILE=${ENV_FILE:-examples/reportportal.env}
SCRIPT=${1:-script.js}

if [ ! -f "$ENV_FILE" ]; then
  echo "Missing $ENV_FILE. Copy examples/reportportal.env.example and fill it in." >&2
  exit 1
fi

if [ -n "${PLUGIN:-}" ]; then
  docker build -f examples/Dockerfile --build-arg "PLUGIN=$PLUGIN" -t "$IMAGE" .
else
  docker build -f examples/Dockerfile -t "$IMAGE" .
fi

docker run --rm --network host --env-file "$ENV_FILE" \
  -v "$PWD/examples:/scripts:ro" \
  "$IMAGE" run --no-usage-report --out reportportal "/scripts/$SCRIPT"

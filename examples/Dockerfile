# Builds k6 with the ReportPortal output and runs it on the official k6 image.
#
# From a checkout of this repository (build context is the repository root):
#   docker build -f examples/Dockerfile -t perf-k6 .
#
# From a release tag, without a checkout:
#   docker build -f examples/Dockerfile \
#     --build-arg PLUGIN=github.com/reportportal/plugin-k6@vX.Y.Z -t perf-k6 .
#
# Another k6 version: --build-arg K6_RELEASE=2.4.0
# (Not named K6_VERSION: xk6 reads that variable itself and rejects it next to a version argument.)
ARG K6_RELEASE=2.4.0

FROM golang:1.26 AS build
ARG K6_RELEASE
ARG PLUGIN=github.com/reportportal/plugin-k6=.
RUN go install go.k6.io/xk6/cmd/xk6@latest
WORKDIR /src
COPY . /src
# A static binary is needed to run in the Alpine-based k6 image.
ENV CGO_ENABLED=0
RUN mkdir -p /out && xk6 build "v${K6_RELEASE}" --output /out/k6 --with "${PLUGIN}"

FROM grafana/k6:${K6_RELEASE}
COPY --from=build /out/k6 /usr/bin/k6

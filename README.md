# k6 ReportPortal output

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![k6](https://img.shields.io/badge/k6-v2-7D64FF?logo=k6&logoColor=white)](https://grafana.com/docs/k6/)
[![ReportPortal](https://img.shields.io/badge/ReportPortal-5.x-39c2d7)](https://reportportal.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

k6 output extension that reports a load test to [ReportPortal](https://reportportal.io) the same way the [JMeter and Gatling performance plugins](https://github.com/reportportal/plugins-performance) do: one launch, aggregated latency percentiles, throughput, an SLA quality gate, and the details of failed requests.

It is a real k6 output (`--out reportportal`), built with [xk6](https://github.com/grafana/xk6). Samples are aggregated in memory with an HdrHistogram. Successful requests are not stored or sent individually.

## What ends up in ReportPortal

One launch per k6 process:

```
Launch  (attributes: p50_ms, p95_ms, p99_ms, throughput_rps, peak_throughput_rps, sla=PASS|FAIL + your custom attributes)
├── Performance Summary Report          STEP   -> SLA table, global metrics, throughput, per-request metrics (Markdown logs)
└── Scenario: <k6 scenario>             SUITE
    └── <group / request name>          SUITE  -> PASSED, or FAILED if it had any failure;
                                              attributes: total, failed, error_rate_pct,
                                              avg_ms, p50_ms, p95_ms, p99_ms
        └── <vu> | <code> | <ms>        STEP   -> failed samples only, with an ERROR log
```

The launch description holds the SLA table, so the quality gate is visible from the launch list. The launch status itself follows that gate: request failures mark the request item FAILED, and the launch is FAILED only when a configured SLA check fails. With no thresholds the launch is PASSED.

k6 scenarios become `Scenario:` suites. A `group()` is prefixed onto the request label (`Checkout / GET /pizza`), same as Gatling groups. Set a stable `name` tag on each request so labels do not contain ids or full URLs.

HTTP and gRPC requests are reported (`http_req_duration`, `grpc_req_duration`). A request is failed when k6 says it failed (`http_req_failed` / `grpc_req_failed`, or `expected_response=false`). By default that is any status outside 200–399, plus network errors. `check()` does not change that on its own.

k6 does not pass request or response bodies to outputs by default. A transport failure is logged from the `error` tag. An HTTP error status has no `error` tag, so the response message is the reason phrase (for example `504` becomes `Gateway Timeout`) plus `error_code` when that tag is enabled. Include `error_code` in `systemTags` for that.

When a request has a `name` tag, k6 copies that name into the `url` tag, so the exact URL is not available to outputs. To show it in failure logs, set a `request_url` tag on the request. The log then gets a `URL:` line under the sampler. Do not put URLs with credentials in that tag. Store `request_url` as a field, not a tag, in InfluxDB (`K6_INFLUXDB_TAGS_AS_FIELDS`).

Request bodies can be sent as a `request_body` tag on each request. The hub enables this with `REPORT_REQUEST_BODIES`. Each request then carries its body, so that tag adds cost on every request, and InfluxDB should be off for that run.

Response bodies cannot be a tag, because k6 sets tags before the response exists. Instead, the script writes an `rp_response_body` counter right after a failed response. Its `response_body` tag holds the body, and `name` and `vu` identify the request. The output holds each failed request until that counter arrives from the same VU, then attaches the body. A held request is released without a body when the same VU makes its next request, or when the test stops. The hub enables this with `REPORT_RESPONSE_BODIES`. Only failed responses are sent, so the extra data stays small. Include `vu` in `systemTags` so the body can be matched to its request. Store `response_body` as a field, not a tag, in InfluxDB (`K6_INFLUXDB_TAGS_AS_FIELDS`), so each body does not create a new series.

A start event is also sent to Google Analytics (agent name `@reportportal/k6`, plugin version, and the k6 version). It uses the same Measurement Protocol property as the JMeter and Gatling plugins, and the same anonymous client id in `~/.rp/rp.properties`. Set `AGENT_NO_ANALYTICS` to any value to skip it.

## ReportPortal setup

The output needs three values: **base URL**, **project name**, and **API token**.

| Correct | Incorrect |
|---|---|
| `https://demo.reportportal.io` | `https://demo.reportportal.io/ui/` |
| `https://rp.example.com` | `https://rp.example.com/api/v1` |

Use the project slug from the address bar (`https://demo.reportportal.io/ui/#my_project/dashboard` → `my_project`), not the numeric id. Create the API token in your user profile. Prefer `https://`. Plain `http://` works, and the output logs a warning because the token is sent unencrypted.

## Install

The extension is compiled into a k6 binary. Install [xk6](https://github.com/grafana/xk6), then from this repository:

```bash
xk6 build --with github.com/reportportal/plugin-performance-k6=.
```

That writes `./k6`. Use that binary instead of a stock k6.

## Run

```bash
export RP_ENDPOINT=https://rp.example.com
export RP_PROJECT=my_project
export RP_API_KEY=your_api_token_here
export RP_LAUNCH="Checkout load test"
export RP_SLA_P95_MS=800
export RP_SLA_ERROR_RATE_PCT=1

./k6 run -o reportportal examples/script.js
```

The same settings can be passed in the output argument. Separate pairs with `;` so values can contain commas:

```bash
./k6 run -o 'reportportal=endpoint=https://rp.example.com;project=my_project;apiKey=your_api_token_here;launch=Checkout load test' examples/script.js
```

Or put them in `reportportal.properties` in the working directory. See [examples/reportportal.properties.example](examples/reportportal.properties.example).

Resolution order, first non-blank wins: `--out` argument, environment variable, `reportportal.properties`. Missing endpoint, token, or project fails the run before the test starts.

If the launch name is left unset, the output uses the script file name. The explicit default, when there is no script name either, is `k6 Performance Metrics`.

Name requests in the script:

```javascript
http.get('https://example.com/api/items', { tags: { name: 'GET /items' } });
```

## Parameters

| Setting | `--out` key | Environment variable | Properties file |
|---|---|---|---|
| Endpoint (required) | `endpoint` | `RP_ENDPOINT` | `rp.endpoint` |
| API token (required) | `apiKey` | `RP_API_KEY`, `RP_TOKEN` | `rp.api.key` |
| Project (required) | `project` | `RP_PROJECT` | `rp.project` |
| Launch name | `launch` | `RP_LAUNCH` | `rp.launch` |
| SLA p95 (ms) | `sla.p95Ms` | `RP_SLA_P95_MS` | `rp.sla.p95.ms` |
| SLA p99 (ms) | `sla.p99Ms` | `RP_SLA_P99_MS` | `rp.sla.p99.ms` |
| SLA error rate (%) | `sla.errorRatePct` | `RP_SLA_ERROR_RATE_PCT` | `rp.sla.error.rate.pct` |
| SLA min throughput (req/s) | `sla.minThroughputRps` | `RP_SLA_MIN_THROUGHPUT_RPS` | `rp.sla.min.throughput.rps` |
| SLA min peak throughput (req/s) | `sla.minPeakThroughputRps` | `RP_SLA_MIN_PEAK_THROUGHPUT_RPS` | `rp.sla.min.peak.throughput.rps` |
| Include labels (regex) | `sample.includeRegex` | `RP_SAMPLE_INCLUDE_REGEX` | `rp.sample.include.regex` |
| Exclude labels (regex) | `sample.excludeRegex` | `RP_SAMPLE_EXCLUDE_REGEX` | `rp.sample.exclude.regex` |
| Peak throughput window (s) | `throughput.windowSizeSeconds` | `RP_THROUGHPUT_WINDOW_SECONDS` | `rp.throughput.window.seconds` |
| Attributes (up to 5) | `attribute.1` … `attribute.5` | `RP_ATTRIBUTE_1` … `RP_ATTRIBUTE_5` | `rp.attribute.1` … `rp.attribute.5` |

Empty SLA values disable that check. The peak window defaults to 1 second. Attributes accept `key:value`, `key=value`, or a bare tag:

```bash
export RP_ATTRIBUTE_1=env:staging
export RP_ATTRIBUTE_2=release=24.3
export RP_ATTRIBUTE_3=nightly
```

Include and exclude are unanchored regular expressions. Exclude is applied first.

Disable analytics:

```bash
export AGENT_NO_ANALYTICS=1
```

## Verify

After the test finishes, open the project in ReportPortal and find the launch.

1. The description is the SLA table when thresholds were set.
2. Launch attributes include `p50_ms`, `p95_ms`, `p99_ms`, `throughput_rps`, `peak_throughput_rps`, and `sla` (`PASS` or `FAIL`) when a threshold was set.
3. **Performance Summary Report** has the Markdown tables.
4. Each request suite has `total`, `failed`, `error_rate_pct`, `avg_ms`, `p50_ms`, `p95_ms`, `p99_ms`.
5. A failed request has up to 20 failed-sample steps. Further failures stay in the counters, and the request item gets a warning log.

## Limitations

- One launch per k6 process. Distributed execution does not merge engines into one launch.
- SLA thresholds are global (p95, p99, error rate, optional minimum overall and peak throughput) and are evaluated once at the end. There are no per-request thresholds.
- Percentiles use an HdrHistogram with 2 significant digits (about 1% error). Response times above 1 hour are clipped to 1 hour. The histogram floor is 1ms, so a sub-millisecond sample is recorded as 1ms.
- At most 1000 distinct request labels. Everything after that is folded into `(other requests)`.
- At most 20 failed samples are reported per label. Response text is truncated at 3000 characters. k6 does not pass bodies to outputs, so a response body appears only when the script sends it with `rp_response_body`.
- At most 5 custom launch attributes.
- WebSocket, browser, and custom trends are not request samples and are not reported.
- `check()` results are not request failures unless the status also fails k6's expected-status rules.

## Build and test

```bash
go test ./...
xk6 build --with github.com/reportportal/plugin-performance-k6=.
```

Go 1.26 or newer, matching k6 v2.

## License

Apache License 2.0 — see [LICENSE](LICENSE).

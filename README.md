# k6 ReportPortal output

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![k6](https://img.shields.io/badge/k6-v2-7D64FF?logo=k6&logoColor=white)](https://grafana.com/docs/k6/)
[![ReportPortal](https://img.shields.io/badge/ReportPortal-5.x-39c2d7)](https://reportportal.io/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A k6 output extension that reports a load test to [ReportPortal](https://reportportal.io) the same way the [JMeter and Gatling performance plugins](https://github.com/reportportal/plugins-performance) do: one launch per test run, latency percentiles, throughput, an SLA quality gate, and the details of failed requests.

You use it as a normal k6 output (`k6 run --out reportportal script.js`). It is compiled into a custom k6 binary with [xk6](https://github.com/grafana/xk6). Samples are aggregated in memory with an HdrHistogram, so successful requests are never stored or sent one by one.

**Contents:** [Quick start](#quick-start) · [Step by step](#step-by-step) · [Recipes](#recipes) · [Reference](#reference) · [Troubleshooting](#troubleshooting)

## What you get

One launch per k6 process:

```
Launch  (p50_ms, p95_ms, p99_ms, throughput_rps, peak_throughput_rps, sla=PASS|FAIL, your attributes)
├── Performance Summary Report          STEP   SLA table, global metrics, throughput, per-request table
└── Scenario: <k6 scenario>             SUITE
    └── <group / request name>          SUITE  PASSED, or FAILED if any request failed
        │                                      total, failed, error_rate_pct, avg_ms, p50_ms, p95_ms, p99_ms
        └── <vu> | <code> | <ms> ms     STEP   one per failed request, with an ERROR log
```

- The launch description is the SLA table, so the quality gate is visible from the launch list.
- The launch is FAILED only when a configured SLA check fails. Failed requests mark their request item FAILED but do not fail the launch. With no thresholds the launch is PASSED. This matches the JMeter and Gatling plugins.
- Each failed request keeps its own step and log: sampler name, URL, thread, status, request body and response body (when your script provides them, see [Failure details](#failure-details-url-request-and-response-bodies)).
- A start event is sent to Google Analytics, like in the JMeter and Gatling plugins. See [Analytics](#analytics).

## Quick start

You need: Go 1.26+, [xk6](https://github.com/grafana/xk6), and a ReportPortal project with an API token.

```bash
# 1. Build a k6 binary that contains the output. This writes ./k6.
xk6 build v2.4.0 --with github.com/reportportal/plugin-k6=.

# 2. Tell the output where to report.
export RP_ENDPOINT=https://rp.example.com
export RP_PROJECT=my_project
export RP_API_KEY=your_api_token_here

# 3. Run any script with the extra output.
./k6 run --out reportportal examples/script.js
```

Open your project in ReportPortal, go to **Launches**, and open the launch named `script` (the script file name). You should see the summary report and one suite per request.

## Step by step

### 1. Get the ReportPortal values

The output needs three values: **base URL**, **project name**, and **API token**.

| Correct | Incorrect |
|---|---|
| `https://demo.reportportal.io` | `https://demo.reportportal.io/ui/` |
| `https://rp.example.com` | `https://rp.example.com/api/v1` |

- **Project:** use the slug from the address bar, not the numeric id. For `https://demo.reportportal.io/ui/#my_project/dashboard` the project is `my_project`.
- **Token:** create an API key in your ReportPortal user profile.
- Prefer `https://`. Plain `http://` works, and the output logs a warning because the token is sent unencrypted.

The ReportPortal that stores the results does not have to be the system you are testing. If you load test ReportPortal itself, keep two separate sets of values: the target you hit with requests, and the instance that receives the performance launch.

### 2. Build a k6 with the output

k6 outputs are compiled in. Use xk6 with the same k6 version as your runtime.

**Local binary:**

```bash
xk6 build v2.4.0 --with github.com/reportportal/plugin-k6=.          # from a checkout
xk6 build v2.4.0 --with github.com/reportportal/plugin-k6@vX.Y.Z     # from a release tag
```

**Docker image** (multi-stage, runs on the official k6 image). Use [examples/Dockerfile](examples/Dockerfile):

```bash
# from a checkout of this repository
docker build -f examples/Dockerfile -t perf-k6 .

# from a release tag, without a checkout
docker build -f examples/Dockerfile \
  --build-arg PLUGIN=github.com/reportportal/plugin-k6@vX.Y.Z -t perf-k6 .
```

Check the result:

```bash
./k6 version
# k6 v2.4.0 (go1.26.4, linux/amd64)
# Extensions:
#   github.com/reportportal/plugin-k6 (devel), reportportal [output]
```

Pin a release tag (`@vX.Y.Z`) in CI so a new commit does not change your tests.

### 3. Prepare the script

The output works with any k6 script. Two habits make the report readable.

**Name every request.** Without a `name` tag k6 uses the full URL as the label, so `/orders/1`, `/orders/2` and so on become separate rows.

```javascript
import http from 'k6/http';
import { check, group, sleep } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'https://quickpizza.grafana.com';

export const options = {
  scenarios: {
    browse: { executor: 'constant-vus', vus: 5, duration: '30s' },
  },
};

export default function () {
  group('Browse', () => {
    const res = http.get(`${BASE_URL}/api/quotes`, {
      tags: { name: 'GET /quotes' },
    });
    check(res, { 'status is 200': (r) => r.status === 200 });
  });
  sleep(1);
}
```

**Use `group()` and scenarios to structure the tree.**

| In the script | In ReportPortal |
|---|---|
| scenario `browse` | suite `Scenario: browse` |
| `group('Browse')` + request `GET /quotes` | request suite `Browse / GET /quotes` |
| nested groups `a::b` | `a / b / GET /quotes` |

This is [examples/script.js](examples/script.js).

### 4. Configure and run

Settings come from three places. The first non-blank value wins:

1. the `--out` argument
2. environment variables
3. a `reportportal.properties` file in the working directory

**Environment variables** (recommended, keeps the token off the command line):

```bash
export RP_ENDPOINT=https://rp.example.com
export RP_PROJECT=my_project
export RP_API_KEY=your_api_token_here
export RP_LAUNCH="Checkout load test"

./k6 run --out reportportal script.js
```

**`--out` argument.** Separate pairs with `;` so values can contain commas:

```bash
./k6 run -o 'reportportal=endpoint=https://rp.example.com;project=my_project;apiKey=your_api_token_here;launch=Checkout load test' script.js
```

**Properties file** (see [examples/reportportal.properties.example](examples/reportportal.properties.example)):

```properties
rp.endpoint=https://rp.example.com
rp.api.key=your_api_token_here
rp.project=my_project
rp.launch=Checkout load test
```

If the launch name is not set, the output uses the script file name, and falls back to `k6 Performance Metrics`. A missing endpoint, token or project fails the run before the test starts. So does a ReportPortal that cannot be reached when the test starts.

Combine it with other outputs freely:

```bash
./k6 run --out influxdb=http://influxdb:8086/k6 --out reportportal script.js
```

### 5. Read the results

Open the launch in ReportPortal.

1. **Launch list.** The description shows the SLA table (when thresholds are set). Attributes show `p50_ms`, `p95_ms`, `p99_ms`, `throughput_rps`, `peak_throughput_rps`, and `sla` (`PASS` or `FAIL`).
2. **Performance Summary Report.** Markdown logs with the SLA table, global metrics, throughput and a per-request table:

   | Request | Total | Success | Failed | Error % | Min (ms) | Max (ms) | Avg (ms) | 50% (ms) | 95% (ms) | 99% (ms) |
   | :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
   | **Browse / GET /quotes** | 150 | 148 | 2 | 1.33% | 41 | 912 | 96 | 82 | 240 | 610 |

3. **Request suites.** Each has `total`, `failed`, `error_rate_pct`, `avg_ms`, `p50_ms`, `p95_ms`, `p99_ms`. A suite is FAILED when it had any failed request.
4. **Failed samples.** Open a FAILED suite to see up to 20 steps named `vu-3 | 504 | 10001 ms`, each with an ERROR log. Further failures stay in the counters, and the suite gets a warning log.

## Examples

The scripts, env file, Dockerfile and mock in [examples/](examples/) were run against this plugin.

| File | What it shows |
|---|---|
| [script.js](examples/script.js) | The smallest useful script: a named request inside a group. |
| [scenarios-and-groups.js](examples/scenarios-and-groups.js) | Two scenarios, groups and a nested group, `vu` in `systemTags`, and k6 thresholds. |
| [failure-details.js](examples/failure-details.js) | URL, request body and response body in failure logs. |
| [reportportal.env.example](examples/reportportal.env.example) | Every setting as an environment variable. Works with `docker --env-file` and with `set -a; . file`. |
| [reportportal.properties.example](examples/reportportal.properties.example) | The same settings as a properties file. |
| [Dockerfile](examples/Dockerfile) | Builds k6 with the output, from a checkout or from a release tag. |
| [run-docker.sh](examples/run-docker.sh) | Builds the image and runs an example script with an env file. |
| [mock-reportportal.py](examples/mock-reportportal.py) | A stand-in ReportPortal API that prints the launch tree. No account needed. |

The scripts use `BASE_URL` (default `https://quickpizza.grafana.com`) as the system under test.

### Try it without a ReportPortal server

The mock accepts any project and token, and prints what a real ReportPortal would receive when the launch finishes: the tree, the SLA table, request attributes, and the first failed step of each request.

```bash
python3 examples/mock-reportportal.py &

RP_ENDPOINT=http://127.0.0.1:8080 RP_PROJECT=demo RP_API_KEY=any-token \
  ./k6 run --out reportportal examples/failure-details.js
```

Stop the mock with `kill %1`. With Docker, put the same three values in `examples/reportportal.env` and run `examples/run-docker.sh failure-details.js`. Use this to check your setup, then switch the endpoint to the real server.

## Recipes

### SLA quality gate

Set any of these. Empty or unset values disable the check:

```bash
export RP_SLA_P95_MS=800                  # global p95 <= 800 ms
export RP_SLA_P99_MS=1500                 # global p99 <= 1500 ms
export RP_SLA_ERROR_RATE_PCT=1            # error rate <= 1 %
export RP_SLA_MIN_THROUGHPUT_RPS=50       # overall mean throughput >= 50 req/s
export RP_SLA_MIN_PEAK_THROUGHPUT_RPS=80  # peak throughput >= 80 req/s
```

The checks run once, at the end, on global numbers. If one fails:

- the launch gets `sla=FAIL` and is FAILED
- the summary log with the SLA table is an ERROR
- the Performance Summary Report item is FAILED

The output cannot change the k6 exit code. To fail a CI job on the same limits, add k6 thresholds too:

```javascript
export const options = {
  thresholds: {
    http_req_duration: ['p(95)<800'],
    http_req_failed: ['rate<0.01'],
  },
};
```

### Custom launch attributes

Up to five, from `RP_ATTRIBUTE_1` to `RP_ATTRIBUTE_5`. They are added when the launch starts. Formats: `key:value`, `key=value`, or a bare tag.

```bash
export RP_ATTRIBUTE_1=env:staging
export RP_ATTRIBUTE_2=release=24.3
export RP_ATTRIBUTE_3=nightly
```

### Leave noise out of the report

Include and exclude are unanchored regular expressions on the request label. Exclude is applied first. Filtered requests are not counted anywhere.

```bash
export RP_SAMPLE_EXCLUDE_REGEX='health|ping'
export RP_SAMPLE_INCLUDE_REGEX='^(Browse|Checkout)'
```

### Peak throughput window

The peak is the highest request rate inside a sliding window. The window defaults to 1 second. Use a wider one for bursty or slow tests:

```bash
export RP_THROUGHPUT_WINDOW_SECONDS=5
```

### Failure details: URL, request and response bodies

k6 does not give outputs the request URL (when you set `name`), the request body or the response body. A failure log would only show the sampler name and status. To get the rest, your script passes it to the output through tags and one counter. This is [examples/failure-details.js](examples/failure-details.js):

```javascript
import http from 'k6/http';
import { Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'https://quickpizza.grafana.com';
const responseBody = new Counter('rp_response_body');

export const options = {
  // vu lets the output match a response body to its request.
  systemTags: [
    'proto', 'status', 'method', 'url', 'name', 'group', 'check',
    'error', 'error_code', 'scenario', 'expected_response', 'vu',
  ],
};

function call(name, method, url, body) {
  const payload = body ? JSON.stringify(body) : null;
  const tags = { name, request_url: url };
  if (payload) tags.request_body = payload.slice(0, 3000);

  const res = http.request(method, url, payload, {
    headers: { 'Content-Type': 'application/json' },
    tags,
  });

  if (res.status === 0 || res.status >= 400) {
    responseBody.add(1, {
      name,
      vu: String(__VU),
      response_body: String(res.body || '').slice(0, 3000),
    });
  }
  return res;
}

export default function () {
  call('POST /api/missing', 'POST', `${BASE_URL}/api/missing`, { size: 'large' });
}
```

The failed step then has this ERROR log:

```
REQUEST FAILURE DETAIL:

URL/Sampler: POST /api/missing
URL: https://example.com/api/missing
Thread: vu-1
Elapsed: 12 ms
Response Code: 404
Response Message: Not Found (error_code 1404)

--- REQUEST BODY ---
{"size":"large"}

--- RESPONSE BODY ---
{"error": "not found"}
```

How each part gets there:

| Log part | Source | What the script does |
|---|---|---|
| `URL/Sampler` | `name` tag | Set `name` on every request. |
| `URL:` | `request_url` tag | Set the tag. k6 copies `name` into `url`, so the real URL is lost otherwise. Never put credentials in it. |
| `Response Code` / `Response Message` | status, `error`, `error_code` | Nothing. For an HTTP error status the message is the reason phrase (`Not Found`) plus `error_code`. A transport failure shows the `error` tag. |
| `REQUEST BODY` | `request_body` tag | Set the tag. It travels with every request, so keep it short. |
| `RESPONSE BODY` | `rp_response_body` counter | After a failed response, add to the counter with tags `name`, `vu` and `response_body`. A tag cannot carry it, because k6 sets tags before the response exists. |

Rules for response bodies:

- Include `vu` in `systemTags`. The output holds each failed request until a counter sample with the same `vu` and `name` arrives. It releases the request without a body when that VU sends its next request, or when the test stops.
- Send the counter only for failures. That keeps the extra data small.
- The body must be available to the script. It is empty with `discardResponseBodies: true` or `responseType: 'none'`.
- A response body is truncated at 3000 characters.
- If you set `systemTags` yourself, keep `name`, `group`, `scenario`, `status`, `method`, `url`, `error`, `error_code`, and add `vu`.

### InfluxDB and Grafana at the same time

Send the same run to InfluxDB for dashboards and to ReportPortal for the report:

```bash
export K6_INFLUXDB_TAGS_AS_FIELDS=url,vu,iter,error,error_code,response_body,request_url,request_body
./k6 run --out influxdb=http://influxdb:8086/k6 --out reportportal script.js
```

`K6_INFLUXDB_TAGS_AS_FIELDS` matters when you use the failure-detail tags above. A tag with a unique value per request creates a new InfluxDB series for each one. Store `request_url`, `request_body`, `response_body` and `vu` as fields so they do not.

k6 2.4 prints a deprecation warning for the InfluxDB output, but it still writes.

### Docker and Jenkins

Build the image with [examples/Dockerfile](examples/Dockerfile) (see the [build step](#2-build-a-k6-with-the-output)), then run it. [examples/run-docker.sh](examples/run-docker.sh) does both with an env file. Or run it yourself. Pass secrets with `-e NAME` (no value), or with `--env-file`, so they stay out of the command line and the process list:

```bash
docker run --rm --network host \
  -e RP_ENDPOINT -e RP_PROJECT -e RP_API_KEY \
  -e RP_LAUNCH="Checkout load test" -e RP_SLA_P95_MS=800 -e RP_ATTRIBUTE_1=env:staging \
  -v "$PWD:/scripts:ro" \
  perf-k6 run --out reportportal /scripts/script.js
```

A Jenkins pipeline uses the same pattern: job parameters become environment variables, and `docker run` forwards them.

```groovy
parameters {
  string(name: 'RP_ENDPOINT', defaultValue: 'https://rp.example.com', description: 'ReportPortal that stores the launch')
  string(name: 'RP_PROJECT',  defaultValue: 'my_project',            description: 'Project for the launch')
  password(name: 'RP_API_KEY', defaultValue: '',                     description: 'API key for that ReportPortal')
  string(name: 'SLA_P95_MS',  defaultValue: '',                      description: 'Empty disables the check')
}
...
withEnv([
  "RP_ENDPOINT=${params.RP_ENDPOINT}",
  "RP_PROJECT=${params.RP_PROJECT}",
  "RP_API_KEY=${params.RP_API_KEY}",
  "RP_LAUNCH=k6 ${env.JOB_NAME}",
  "RP_SLA_P95_MS=${params.SLA_P95_MS}",
  "RP_ATTRIBUTE_1=build:${env.BUILD_NUMBER}",
]) {
  sh '''
    docker run --rm -e RP_ENDPOINT -e RP_PROJECT -e RP_API_KEY -e RP_LAUNCH \
      -e RP_SLA_P95_MS -e RP_ATTRIBUTE_1 \
      perf-k6 run --out reportportal /scripts/script.js
  '''
}
```

Tips for pipelines:

- Validate the parameters before the run. An invalid endpoint or key then fails fast with a clear message.
- Put build metadata in `RP_ATTRIBUTE_n`, so launches can be filtered by environment, test type and build in ReportPortal.
- Keep the ReportPortal parameters separate from the target parameters.
- Mark the stage UNSTABLE instead of failing when k6 thresholds fail, so the report still gets published.

### Analytics

On start the output sends one event to Google Analytics: agent name `@reportportal/k6`, the plugin version and the k6 version. It uses the same Measurement Protocol property as the JMeter and Gatling plugins and the same anonymous client id in `~/.rp/rp.properties`. The request is best-effort and never affects the test.

To turn it off:

```bash
export AGENT_NO_ANALYTICS=1
```

## Reference

### Parameters

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

Keys are case-insensitive. Empty SLA values disable that check. The peak window defaults to 1 second. Setting `AGENT_NO_ANALYTICS` to any value disables [analytics](#analytics).

### What counts as a failed request

HTTP and gRPC requests are reported (`http_req_duration`, `grpc_req_duration`).

- **HTTP:** failed when k6 says so: `http_req_failed`, or `expected_response=false`, or an `error` tag. By default that is any status outside 200–399, plus network errors. Change it with `http.setResponseCallback(http.expectedStatuses(...))`.
- **gRPC:** failed when the status code is not 0.
- `check()` results do not change this on their own.

### What the output reads from your script

| Name | Kind | Used for |
|---|---|---|
| `name` | request tag | request label. Without it the URL is the label. |
| `group` | k6 group | prefix of the label, joined with ` / ` |
| `scenario` | k6 scenario | the `Scenario:` suite |
| `vu` | system tag | thread name (`vu-3`) and response body matching. Add it to `systemTags`. |
| `status`, `error`, `error_code` | system tags | response code and message in failure logs |
| `request_url` | your tag | `URL:` line in the failure log |
| `request_body` | your tag | request body in the failure log |
| `rp_response_body` | your counter | response body in the failure log (tags `name`, `vu`, `response_body`) |

### Limitations

- One launch per k6 process. Distributed execution does not merge engines into one launch.
- SLA thresholds are global and evaluated once at the end. There are no per-request thresholds.
- Percentiles use an HdrHistogram with 2 significant digits (about 1% error). Response times above 1 hour are clipped to 1 hour. The histogram floor is 1 ms, so a sub-millisecond sample is recorded as 1 ms.
- At most 1000 distinct request labels. Everything after that is folded into `(other requests)`.
- At most 20 failed samples per label. Response text is truncated at 3000 characters.
- At most 5 custom launch attributes.
- WebSocket, browser and custom trends are not request samples and are not reported.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `invalid output type 'reportportal'` | You are running a stock k6. Run the binary or image built with xk6. |
| Run stops at start with `endpoint`, `API token` or `project` is required | Set them. The names are `RP_ENDPOINT`, `RP_API_KEY` (or `RP_TOKEN`) and `RP_PROJECT`. |
| Run stops at start with `ReportPortal POST v1/<project>/launch returned HTTP 401` (or 403, 404) | Check the token, and that the project is the slug (`my_project`), not the numeric id. The endpoint must not end in `/ui` or `/api/v1`. |
| Failed steps show the thread as `vu` instead of `vu-3` | `vu` is not a default k6 system tag. Add it to `systemTags`. |
| Report has one row per URL | The requests have no `name` tag. Set one, so ids stay out of the label. |
| Failed steps say `No Request Data` or `No Response Data` | k6 does not pass bodies to outputs. Set `request_body` and `rp_response_body` as in [Failure details](#failure-details-url-request-and-response-bodies). |
| No `URL:` line in the failure log | Set the `request_url` tag. k6 copies `name` into `url`. |
| Response body is missing even with the counter | Add `vu` to `systemTags`. Make sure the body is not discarded (`discardResponseBodies`, `responseType: 'none'`). The tags `name` and `vu` on the counter must match the request. |
| `Response Message` is only `Bad Request (error_code 1400)` | That is the reason phrase plus `error_code`. The server's own text is available only through `rp_response_body`. |
| Launch is PASSED but requests failed | By design. The launch follows the SLA only. Set SLA values, and read the FAILED request suites. |
| No `sla` attribute | No SLA threshold is configured. |
| Log says `attributes update skipped` | The output could not read the launch back at the end. The launch is still finished. Check the token's permissions on the project. |
| Many failed requests but few steps | Only 20 failed samples are kept per label. The counters have the full numbers. |
| Rows named `(other requests)` | More than 1000 distinct labels. Use stable `name` tags. |
| Nothing from some requests | They may be matched by `RP_SAMPLE_EXCLUDE_REGEX`, or they are WebSocket, browser or custom metrics. |
| InfluxDB grows fast after adding the body tags | Put `request_url`, `request_body`, `response_body` and `vu` in `K6_INFLUXDB_TAGS_AS_FIELDS`. |

## Build and test

```bash
go test ./...
xk6 build --with github.com/reportportal/plugin-k6=.
```

Go 1.26 or newer, matching k6 v2.

## License

Apache License 2.0. See [LICENSE](LICENSE).

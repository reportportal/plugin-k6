import http from 'k6/http';
import { check, group, sleep } from 'k6';

// BASE_URL is the system under test, not ReportPortal.
const BASE_URL = __ENV.BASE_URL || 'https://quickpizza.grafana.com';
const DURATION = __ENV.DURATION || '20s';

// Two scenarios become two suites in ReportPortal: "Scenario: browse" and "Scenario: checkout".
export const options = {
  scenarios: {
    browse: {
      executor: 'constant-vus',
      exec: 'browse',
      vus: 3,
      duration: DURATION,
    },
    checkout: {
      executor: 'constant-vus',
      exec: 'checkout',
      vus: 2,
      duration: DURATION,
    },
  },
  // vu names the failed-request steps "vu-3 | 500 | 12 ms". It is not a default k6 system tag.
  // In InfluxDB, store it as a field (K6_INFLUXDB_TAGS_AS_FIELDS=vu) so it does not add series.
  systemTags: [
    'proto', 'status', 'method', 'url', 'name', 'group', 'check',
    'error', 'error_code', 'scenario', 'expected_response', 'vu',
  ],
  // The output cannot change the k6 exit code. Use thresholds to fail a CI job on the same limits
  // you set as RP_SLA_* values.
  thresholds: {
    http_req_duration: ['p(95)<800'],
    http_req_failed: ['rate<0.01'],
  },
};

// Every request has a stable name, so ids and query strings do not create one row per URL.
function get(name, path) {
  return http.get(`${BASE_URL}${path}`, { tags: { name } });
}

export function browse() {
  group('Browse', () => {
    check(get('GET /', '/'), { 'home page is 200': (r) => r.status === 200 });
    check(get('GET /api/quotes', '/api/quotes'), { 'quotes are 200': (r) => r.status === 200 });
  });
  sleep(1);
}

export function checkout() {
  group('Checkout', () => {
    check(get('GET /api/names', '/api/names'), { 'names are 200': (r) => r.status === 200 });

    // A nested group is joined with " / ": "Checkout / Payment / GET /api/quotes".
    group('Payment', () => {
      check(get('GET /api/quotes', '/api/quotes'), { 'quotes are 200': (r) => r.status === 200 });
    });
  });
  sleep(1);
}

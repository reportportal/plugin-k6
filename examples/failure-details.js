import http from 'k6/http';
import { Counter } from 'k6/metrics';
import { sleep } from 'k6';

// BASE_URL is the system under test, not ReportPortal.
const BASE_URL = __ENV.BASE_URL || 'https://quickpizza.grafana.com';

// Failed responses are sent to the ReportPortal output through this counter.
const responseBody = new Counter('rp_response_body');

export const options = {
  vus: 2,
  duration: '10s',
  // vu lets the output match a response body to its request.
  systemTags: [
    'proto', 'status', 'method', 'url', 'name', 'group', 'check',
    'error', 'error_code', 'scenario', 'expected_response', 'vu',
  ],
};

// One place that adds everything a failure log can show.
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
  call('GET /', 'GET', `${BASE_URL}/`);
  call('POST /api/missing', 'POST', `${BASE_URL}/api/missing`, { size: 'large' });
  sleep(1);
}

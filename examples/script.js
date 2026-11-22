import http from 'k6/http';
import { check, group, sleep } from 'k6';

// BASE_URL is the system under test, not ReportPortal.
const BASE_URL = __ENV.BASE_URL || 'https://quickpizza.grafana.com';

export const options = {
  scenarios: {
    browse: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
    },
  },
};

export default function () {
  group('Browse', () => {
    // A stable name keeps ids and query strings out of the ReportPortal label.
    const res = http.get(`${BASE_URL}/api/quotes`, {
      tags: { name: 'GET /quotes' },
    });
    check(res, { 'status is 200': (r) => r.status === 200 });
  });
  sleep(1);
}

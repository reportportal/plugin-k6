import http from 'k6/http';
import { check, group, sleep } from 'k6';

export const options = {
  scenarios: {
    checkout: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
    },
  },
};

export default function () {
  group('Checkout', () => {
    const pizza = http.get('https://quickpizza.grafana.com/api/pizza', {
      tags: { name: 'GET /pizza' },
    });
    check(pizza, { 'pizza status is 200': (r) => r.status === 200 });
  });
  sleep(1);
}

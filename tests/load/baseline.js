import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';

// -- Configuration ----------------------------------------------------------

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const REDIRECT_URL = __ENV.REDIRECT_URL || 'http://localhost:8081';

// Pre-seeded short codes for redirect testing (created by seed script)
const SHORT_CODES = (__ENV.SHORT_CODES || 'test001,test002,test003,test004,test005,test006,test007,test008,test009,test010').split(',');

// -- Custom Metrics ---------------------------------------------------------

const redirectErrors = new Rate('redirect_errors');
const createErrors = new Rate('create_errors');
const statsErrors = new Rate('stats_errors');
const redirectDuration = new Trend('redirect_duration', true);
const createDuration = new Trend('create_duration', true);
const statsDuration = new Trend('stats_duration', true);

// -- k6 Options -------------------------------------------------------------

export const options = {
    scenarios: {
        baseline_load: {
            executor: 'ramping-arrival-rate',
            startRate: 0,
            timeUnit: '1s',
            preAllocatedVUs: 200,
            maxVUs: 500,
            stages: [
                { duration: '30s', target: 1000 },  // ramp up to 1,000 RPS over 30s
                { duration: '5m', target: 1000 },    // hold at 1,000 RPS for 5 minutes
                { duration: '10s', target: 0 },      // ramp down
            ],
        },
    },

    thresholds: {
        'redirect_duration': ['p(99)<100'],   // p99 redirect < 100ms
        'create_duration': ['p(99)<300'],     // p99 create < 300ms
        'stats_duration': ['p(99)<500'],      // p99 stats < 500ms
        'redirect_errors': ['rate<0.001'],    // < 0.1% redirect errors
        'create_errors': ['rate<0.01'],       // < 1% create errors
        'http_req_failed': ['rate<0.01'],     // < 1% overall HTTP failures
    },

    tags: {
        test_type: 'baseline',
    },
};

// -- Helpers ----------------------------------------------------------------

function randomCode() {
    return SHORT_CODES[Math.floor(Math.random() * SHORT_CODES.length)];
}

function randomURL() {
    const id = Math.floor(Math.random() * 1000000);
    return `https://example.com/page/${id}`;
}

// -- Traffic Mix: 80% redirect, 15% create, 5% stats -----------------------

export default function () {
    const roll = Math.random();

    if (roll < 0.80) {
        // -- Redirect (80%) --
        doRedirect();
    } else if (roll < 0.95) {
        // -- Create (15%) --
        doCreate();
    } else {
        // -- Stats (5%) --
        doStats();
    }
}

function doRedirect() {
    const code = randomCode();
    const res = http.get(`${REDIRECT_URL}/${code}`, {
        redirects: 0,  // do not follow redirects, measure raw response
        tags: { name: 'redirect' },
    });

    redirectDuration.add(res.timings.duration);

    const ok = check(res, {
        'redirect: status is 302 or 404 or 410': (r) =>
            [302, 404, 410].includes(r.status),
    });

    redirectErrors.add(!ok);
}

function doCreate() {
    const payload = JSON.stringify({
        original_url: randomURL(),
    });

    const res = http.post(`${BASE_URL}/api/v1/shorten`, payload, {
        headers: { 'Content-Type': 'application/json' },
        tags: { name: 'create' },
    });

    createDuration.add(res.timings.duration);

    const ok = check(res, {
        'create: status is 201 or 429': (r) =>
            [201, 429].includes(r.status),
    });

    createErrors.add(!ok);
}

function doStats() {
    const code = randomCode();
    const res = http.get(`${BASE_URL}/api/v1/links/${code}/stats`, {
        tags: { name: 'stats' },
    });

    statsDuration.add(res.timings.duration);

    const ok = check(res, {
        'stats: status is 200 or 401 or 403 or 404': (r) =>
            [200, 401, 403, 404].includes(r.status),
    });

    statsErrors.add(!ok);
}

// -- Summary Output ---------------------------------------------------------

export function handleSummary(data) {
    return {
        'stdout': textSummary(data, { indent: '  ', enableColors: true }),
        'tests/load/results/baseline-summary.json': JSON.stringify(data, null, 2),
    };
}

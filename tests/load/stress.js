import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';

// -- Configuration ----------------------------------------------------------

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const REDIRECT_URL = __ENV.REDIRECT_URL || 'http://localhost:8081';

const SHORT_CODES = (__ENV.SHORT_CODES || 'test001,test002,test003,test004,test005,test006,test007,test008,test009,test010').split(',');

// -- Custom Metrics ---------------------------------------------------------

const redirectErrors = new Rate('redirect_errors');
const createErrors = new Rate('create_errors');
const statsErrors = new Rate('stats_errors');
const redirectDuration = new Trend('redirect_duration', true);
const createDuration = new Trend('create_duration', true);
const statsDuration = new Trend('stats_duration', true);
const errorCount = new Counter('total_errors');
const breakingPointReached = new Rate('breaking_point_reached');

// -- k6 Options -------------------------------------------------------------

export const options = {
    scenarios: {
        stress_ramp: {
            executor: 'ramping-arrival-rate',
            startRate: 0,
            timeUnit: '1s',
            preAllocatedVUs: 500,
            maxVUs: 5000,
            stages: [
                { duration: '2m', target: 10000 },  // ramp from 0 to 10,000 RPS over 2 min
                { duration: '5m', target: 10000 },   // hold at 10,000 RPS for 5 min
                { duration: '2m', target: 0 },        // ramp down over 2 min
            ],
        },
    },

    thresholds: {
        // Stress test thresholds are relaxed compared to baseline.
        // The goal is to find the breaking point, not to enforce SLAs.
        'redirect_duration': ['p(99)<200'],   // p99 redirect < 200ms (2x baseline)
        'http_req_failed': ['rate<0.05'],     // < 5% overall failure acceptable under stress
    },

    tags: {
        test_type: 'stress',
    },
};

// -- Helpers ----------------------------------------------------------------

function randomCode() {
    return SHORT_CODES[Math.floor(Math.random() * SHORT_CODES.length)];
}

function randomURL() {
    const id = Math.floor(Math.random() * 1000000);
    return `https://example.com/stress/${id}`;
}

// -- Traffic Mix: 80% redirect, 15% create, 5% stats -----------------------

export default function () {
    const roll = Math.random();

    if (roll < 0.80) {
        doRedirect();
    } else if (roll < 0.95) {
        doCreate();
    } else {
        doStats();
    }
}

function doRedirect() {
    const code = randomCode();
    const res = http.get(`${REDIRECT_URL}/${code}`, {
        redirects: 0,
        tags: { name: 'redirect' },
        timeout: '10s',
    });

    redirectDuration.add(res.timings.duration);

    const ok = check(res, {
        'redirect: status is 302 or 404 or 410 or 429': (r) =>
            [302, 404, 410, 429].includes(r.status),
    });

    if (!ok) {
        redirectErrors.add(true);
        errorCount.add(1);
    } else {
        redirectErrors.add(false);
    }

    // Track when errors start appearing (breaking point indicator)
    if (res.status >= 500 || res.status === 0) {
        breakingPointReached.add(true);
    } else {
        breakingPointReached.add(false);
    }
}

function doCreate() {
    const payload = JSON.stringify({
        original_url: randomURL(),
    });

    const res = http.post(`${BASE_URL}/api/v1/shorten`, payload, {
        headers: { 'Content-Type': 'application/json' },
        tags: { name: 'create' },
        timeout: '10s',
    });

    createDuration.add(res.timings.duration);

    const ok = check(res, {
        'create: status is 201 or 429': (r) =>
            [201, 429].includes(r.status),
    });

    if (!ok) {
        createErrors.add(true);
        errorCount.add(1);
    } else {
        createErrors.add(false);
    }

    if (res.status >= 500 || res.status === 0) {
        breakingPointReached.add(true);
    } else {
        breakingPointReached.add(false);
    }
}

function doStats() {
    const code = randomCode();
    const res = http.get(`${BASE_URL}/api/v1/links/${code}/stats`, {
        tags: { name: 'stats' },
        timeout: '10s',
    });

    statsDuration.add(res.timings.duration);

    const ok = check(res, {
        'stats: status is 200 or 401 or 403 or 404 or 429': (r) =>
            [200, 401, 403, 404, 429].includes(r.status),
    });

    if (!ok) {
        statsErrors.add(true);
        errorCount.add(1);
    } else {
        statsErrors.add(false);
    }

    if (res.status >= 500 || res.status === 0) {
        breakingPointReached.add(true);
    } else {
        breakingPointReached.add(false);
    }
}

// -- Summary Output ---------------------------------------------------------

export function handleSummary(data) {
    // Extract breaking point information
    const breakingInfo = {};
    if (data.metrics && data.metrics.breaking_point_reached) {
        breakingInfo.breaking_point_error_rate = data.metrics.breaking_point_reached.values.rate;
    }
    if (data.metrics && data.metrics.total_errors) {
        breakingInfo.total_errors = data.metrics.total_errors.values.count;
    }

    const summary = Object.assign({}, data, { breaking_point_analysis: breakingInfo });

    return {
        'stdout': textSummary(data, { indent: '  ', enableColors: true }),
        'tests/load/results/stress-summary.json': JSON.stringify(summary, null, 2),
    };
}

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter, Gauge } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';

// -- Configuration ----------------------------------------------------------

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const REDIRECT_URL = __ENV.REDIRECT_URL || 'http://localhost:8081';

const SHORT_CODES = (__ENV.SHORT_CODES || 'test001,test002,test003,test004,test005,test006,test007,test008,test009,test010').split(',');

// Soak test duration can be overridden for shorter CI runs
const SOAK_DURATION = __ENV.SOAK_DURATION || '30m';
const SOAK_RPS = parseInt(__ENV.SOAK_RPS || '500');

// -- Custom Metrics ---------------------------------------------------------

const redirectErrors = new Rate('redirect_errors');
const createErrors = new Rate('create_errors');
const statsErrors = new Rate('stats_errors');
const redirectDuration = new Trend('redirect_duration', true);
const createDuration = new Trend('create_duration', true);
const statsDuration = new Trend('stats_duration', true);

// Track error rate over time to detect degradation
const errorCounter = new Counter('total_errors');
const requestCounter = new Counter('total_requests');

// -- k6 Options -------------------------------------------------------------
//
// Soak test profile:
//   - Gentle ramp to 500 RPS over 1 minute
//   - Hold at 500 RPS for 30 minutes
//   - Gentle ramp down over 30 seconds
//
// Purpose: detect memory leaks, connection pool exhaustion,
// resource accumulation, and error rate increase over time.

export const options = {
    scenarios: {
        soak: {
            executor: 'ramping-arrival-rate',
            startRate: 0,
            timeUnit: '1s',
            preAllocatedVUs: 100,
            maxVUs: 300,
            stages: [
                { duration: '1m', target: SOAK_RPS },   // gentle ramp up
                { duration: SOAK_DURATION, target: SOAK_RPS },  // sustained load
                { duration: '30s', target: 0 },           // ramp down
            ],
        },
    },

    thresholds: {
        // Soak thresholds are strict -- at moderate load, everything should be stable.
        'redirect_duration': ['p(99)<100'],    // p99 redirect < 100ms (same as baseline)
        'create_duration': ['p(99)<300'],      // p99 create < 300ms
        'redirect_errors': ['rate<0.001'],     // < 0.1% redirect errors
        'create_errors': ['rate<0.01'],        // < 1% create errors
        'http_req_failed': ['rate<0.005'],     // < 0.5% overall failure
    },

    tags: {
        test_type: 'soak',
    },
};

// -- Helpers ----------------------------------------------------------------

function randomCode() {
    return SHORT_CODES[Math.floor(Math.random() * SHORT_CODES.length)];
}

function randomURL() {
    const id = Math.floor(Math.random() * 1000000);
    return `https://example.com/soak/${id}`;
}

// -- Traffic Mix: 80% redirect, 15% create, 5% stats -----------------------

export default function () {
    requestCounter.add(1);
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
        'redirect: valid status': (r) =>
            [302, 404, 410, 429].includes(r.status),
    });

    redirectErrors.add(!ok);
    if (!ok) errorCounter.add(1);

    // Detect latency degradation (potential memory leak / pool exhaustion)
    if (res.timings.duration > 500) {
        check(res, {
            'redirect: latency not degraded (< 500ms)': () => false,
        });
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
        'create: valid status': (r) =>
            [201, 429].includes(r.status),
    });

    createErrors.add(!ok);
    if (!ok) errorCounter.add(1);

    // Detect latency degradation
    if (res.timings.duration > 1000) {
        check(res, {
            'create: latency not degraded (< 1000ms)': () => false,
        });
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
        'stats: valid status': (r) =>
            [200, 401, 403, 404, 429].includes(r.status),
    });

    statsErrors.add(!ok);
    if (!ok) errorCounter.add(1);
}

// -- Summary Output ---------------------------------------------------------

export function handleSummary(data) {
    // Analyze error rate trend over time for degradation detection
    const soakAnalysis = {
        total_duration: SOAK_DURATION,
        target_rps: SOAK_RPS,
    };

    if (data.metrics) {
        if (data.metrics.total_errors) {
            soakAnalysis.total_errors = data.metrics.total_errors.values.count;
        }
        if (data.metrics.total_requests) {
            soakAnalysis.total_requests = data.metrics.total_requests.values.count;
        }
        if (data.metrics.redirect_duration) {
            soakAnalysis.redirect_p99 = data.metrics.redirect_duration.values['p(99)'];
            soakAnalysis.redirect_p50 = data.metrics.redirect_duration.values['p(50)'];
            soakAnalysis.redirect_max = data.metrics.redirect_duration.values.max;
        }
        if (data.metrics.create_duration) {
            soakAnalysis.create_p99 = data.metrics.create_duration.values['p(99)'];
            soakAnalysis.create_max = data.metrics.create_duration.values.max;
        }

        // Flag potential issues
        soakAnalysis.warnings = [];
        if (soakAnalysis.redirect_max > 2000) {
            soakAnalysis.warnings.push('Redirect max latency > 2s -- possible connection pool exhaustion');
        }
        if (soakAnalysis.create_max > 5000) {
            soakAnalysis.warnings.push('Create max latency > 5s -- possible resource leak');
        }
        if (soakAnalysis.total_errors > 0 && soakAnalysis.total_requests > 0) {
            const errorRate = soakAnalysis.total_errors / soakAnalysis.total_requests;
            if (errorRate > 0.001) {
                soakAnalysis.warnings.push(`Error rate ${(errorRate * 100).toFixed(3)}% exceeds 0.1% threshold`);
            }
        }
    }

    const summary = Object.assign({}, data, { soak_analysis: soakAnalysis });

    return {
        'stdout': textSummary(data, { indent: '  ', enableColors: true }),
        'tests/load/results/soak-summary.json': JSON.stringify(summary, null, 2),
    };
}

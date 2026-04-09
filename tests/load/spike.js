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
const redirectDuration = new Trend('redirect_duration', true);
const createDuration = new Trend('create_duration', true);
const spikeErrors = new Counter('spike_phase_errors');
const recoveryErrors = new Counter('recovery_phase_errors');

// -- k6 Options -------------------------------------------------------------
//
// Spike test profile:
//   1. Instant surge to 5,000 RPS (5s ramp to simulate near-instant)
//   2. Hold at 5,000 RPS for 1 minute
//   3. Drop to 500 RPS (5s ramp down)
//   4. Hold at 500 RPS for 1 minute (recovery observation)
//   5. Ramp down to 0

export const options = {
    scenarios: {
        spike: {
            executor: 'ramping-arrival-rate',
            startRate: 0,
            timeUnit: '1s',
            preAllocatedVUs: 500,
            maxVUs: 3000,
            stages: [
                { duration: '5s', target: 5000 },    // near-instant surge to 5,000 RPS
                { duration: '1m', target: 5000 },     // hold spike for 1 minute
                { duration: '5s', target: 500 },      // drop to 500 RPS
                { duration: '1m', target: 500 },      // hold recovery for 1 minute
                { duration: '5s', target: 0 },         // ramp down
            ],
        },
    },

    thresholds: {
        // During spike, we expect some degradation but track recovery.
        'redirect_duration': ['p(99)<500'],    // relaxed p99 during spike
        'redirect_errors': ['rate<0.05'],      // < 5% error rate
        'http_req_failed': ['rate<0.05'],      // < 5% overall failure
    },

    tags: {
        test_type: 'spike',
    },
};

// -- Helpers ----------------------------------------------------------------

function randomCode() {
    return SHORT_CODES[Math.floor(Math.random() * SHORT_CODES.length)];
}

function randomURL() {
    const id = Math.floor(Math.random() * 1000000);
    return `https://example.com/spike/${id}`;
}

// Track which phase we are in based on elapsed time
function getPhase() {
    // Approximate phase detection based on test execution time
    // __ITER is not reliable for arrival-rate executors, so we use VU-local time
    const elapsed = (__ENV.__TEST_START)
        ? (Date.now() - parseInt(__ENV.__TEST_START)) / 1000
        : 0;

    if (elapsed < 65) return 'spike';       // 5s ramp + 60s hold
    if (elapsed < 130) return 'recovery';    // 5s drop + 60s hold
    return 'cooldown';
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
        'redirect: valid status': (r) =>
            [302, 404, 410, 429].includes(r.status),
    });

    redirectErrors.add(!ok);

    if (!ok) {
        const phase = getPhase();
        if (phase === 'spike') {
            spikeErrors.add(1);
        } else if (phase === 'recovery') {
            recoveryErrors.add(1);
        }
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
}

function doStats() {
    const code = randomCode();
    const res = http.get(`${BASE_URL}/api/v1/links/${code}/stats`, {
        tags: { name: 'stats' },
        timeout: '10s',
    });

    check(res, {
        'stats: valid status': (r) =>
            [200, 401, 403, 404, 429].includes(r.status),
    });
}

// -- Summary Output ---------------------------------------------------------

export function handleSummary(data) {
    const recoveryAnalysis = {};
    if (data.metrics && data.metrics.spike_phase_errors) {
        recoveryAnalysis.spike_phase_errors = data.metrics.spike_phase_errors.values.count;
    }
    if (data.metrics && data.metrics.recovery_phase_errors) {
        recoveryAnalysis.recovery_phase_errors = data.metrics.recovery_phase_errors.values.count;
    }

    const summary = Object.assign({}, data, { recovery_analysis: recoveryAnalysis });

    return {
        'stdout': textSummary(data, { indent: '  ', enableColors: true }),
        'tests/load/results/spike-summary.json': JSON.stringify(summary, null, 2),
    };
}

# Shorty Dependency Graph

## Full Task Dependency Graph (ASCII)

```
SPRINT 0 -- Foundation                                            SPRINT 1 -- Infrastructure
============================================                      ============================================

T01(pm)─────────────────────────┐
  backlog, MVP scope            │
  [~M effort]                   │
                                ├──► T07(planner)
T02(architect)──┬───────────────┘      sprint plan
  openapi.yaml  │                      [~M effort]
  [~L effort]   │
                ├──► T03(architect)
                │      ADRs 001-010
                │      [~M effort]
                │
                └──► T04(architect)──┬──► T05(dba)
                       data model    │     DynamoDB patterns
                       IAM matrix    │     [~M effort]
                       [~M effort]   │
                                     └──► T06(dba)
                                           Redis design
                                           [~M effort]
                          │
                    [S0 GATE: spec-validate PASS]
                    [All docs exist, no table scans]
                          │
                          ▼
T01(aws)────┐   T04(sec)──► T05(sec)     T09(designer)
T02(aws)────┼──► T07(devops)──► T08(devops)
T03(aws)────┘      TF modules     envs+CI/CD
                   [~XL]          [~L]
T06(devops) ◄── T01(aws)
  docker-compose
                          │
                    [S1 GATE: infra files exist]
                    [Docker Compose, 9 TF modules, CI workflow]
                          │
                          ▼

SPRINT 2 -- BDD Red State                 SPRINT 3 -- Core Implementation
==========================================  ============================================

T01(go-dev)─────────┐
  project skeleton   │
  [~M effort]        │
                     ├──► T02(qa) ──────┐
                     │    redirect,      │
                     │    create, rate   │
                     │    [~M effort]    ├──► T04(qa)
                     │                   │    step defs + E2E
                     ├──► T03(qa) ──────┘    [~L effort]
                     │    password, TTL,
                     │    stats
                     │    [~M effort]
                     │
                     └──► T05(qa)
                          test plan, k6
                          [~M effort]
                          │
                    [S2 GATE: 6 .feature files]
                    [go build compiles, BDD FAIL (red)]
                          │
                          ▼
                    T01(go-dev)──────────────────┐
                      spec-gen + store + cache   │
                      [~L effort]                │
                                                 ├──► T02(go-dev) ──┐
                                                 │    shortener      │
                                                 │    + validator    │
                                                 │    [~M effort]    │
                                                 │                   ├──► T04(go-dev)
                                                 └──► T03(go-dev) ──┤    cmd/redirect
                                                      ratelimit     │    [~L effort]
                                                      [~M effort]   │
                                                                    ├──► T05(go-dev)
                                                                    │    cmd/api
                                                                    │    [~L effort]
                                                                    │
                                                                    └──► T06(go-dev)
                                                                         telemetry+geo
                                                                         +mocks, BDD green
                                                                         [~M effort]
                          │
                    [S3 GATE: make bdd GREEN]
                    [make test PASS, coverage >= 80%]
                    [make build produces 3 zips]
                          │
                          ▼

SPRINT 4 -- Review + Observability        SPRINT 5 -- Stats, Auth, Worker
==========================================  ============================================

T01(reviewer)──► T06?(go-dev)             T01(go-dev) ──────┐
  review S3       fix blockers              cmd/worker       │
  [~L effort]     [conditional]             [~M effort]      │
                                                             ├──► T03(go-dev)
T02(sre) ──► T03(sre)                    T02(go-dev) ──────┘    stats API
  SLO/SLI     dashboards+alerts            internal/auth         [~L effort]
  [~M]        [~L]                         [~L effort]              │
                                                                    ▼
T04(perf) ──► T05(perf)                                       T04(qa)
  critical     redis/dynamo/CF               integration tests
  path         perf analysis                 [~M effort]
  [~M]         [~M]                             │
                          │                     ▼
                    [S4 GATE: zero blockers]  T05(reviewer)──► T06?(go-dev)
                    [dashboards exist]         review S5        fix blockers
                    [perf docs exist]          [~M]             [conditional]
                          │                     │
                          ▼               [S5 GATE: test-all GREEN]
                                          [zero blockers, worker+auth wired]
                                                │
                                                ▼

SPRINT 6 -- Hardening & Launch
==========================================

T01(qa) ──────────► T03(perf) ──► T04(sre)
  k6 load tests       interpret     capacity plan
  [~L effort]          results       chaos design
                       [~M]          [~M]

T02(qa)
  security scans
  [~M effort]

T05(devops) ──► T06(devops)
  prod TF         seed+migrate
  [~L effort]     [~M]

                    │
              T07(planner)
                final gate
                review ALL
                [~M effort]
                    │
              [FINAL GATE: launch ready]
              [test-all + check + build PASS]
              [p99 <= 100ms @ 1K RPS]
              [zero CRITICAL/HIGH gosec]
              [all P0 user stories covered]
```

---

## Critical Path

The critical path is the longest sequential chain of dependencies determining minimum project duration.

```
S0-T02 ──► S0-T04 ──► [S0 GATE] ──► S1-T01 ──► S1-T07 ──► S1-T08 ──► [S1 GATE]
  L           M                        M           XL          L
                                                                          │
──► S2-T01 ──► S2-T02 ──► S2-T04 ──► [S2 GATE] ──► S3-T01 ──► S3-T02 ──┘
      M          M           L                        L           M
                                                                  │
──► S3-T04 ──► S3-T06 ──► [S3 GATE] ──► S4-T01 ──► S4-T06? ──► [S4 GATE]
      L           M                        L          S-L
                                                                  │
──► S5-T01 ──► S5-T03 ──► S5-T04 ──► S5-T05 ──► [S5 GATE]      │
      M           L          M          M                         │
                                                                  │
──► S6-T01 ──► S6-T03 ──► S6-T04 ──► S6-T07 ──► [FINAL GATE]   │
      L           M          M          M
```

### Critical Path Timing Estimate

| Segment | Tasks | Effort Sum | Estimated Duration |
|---|---|---|---|
| Sprint 0 (critical chain) | T02 -> T04 | L + M | ~1.5 sessions |
| Sprint 1 (critical chain) | T01 -> T07 -> T08 | M + XL + L | ~2.5 sessions |
| Sprint 2 (critical chain) | T01 -> T02 -> T04 | M + M + L | ~1.5 sessions |
| Sprint 3 (critical chain) | T01 -> T02 -> T04 -> T06 | L + M + L + M | ~3 sessions |
| Sprint 4 (critical chain) | T01 -> T06? | L + (S-L) | ~1.5 sessions |
| Sprint 5 (critical chain) | T01 -> T03 -> T04 -> T05 | M + L + M + M | ~2 sessions |
| Sprint 6 (critical chain) | T01 -> T03 -> T04 -> T07 | L + M + M + M | ~2 sessions |
| **Total critical path** | | | **~14 sessions** |

---

## Parallel Tracks

These tracks can execute independently from the critical path within their respective sprints:

### Sprint 0 Parallel Tracks
- **Track A (critical):** T02 -> T04 -> T05/T06
- **Track B:** T01 (PM work, independent)
- **Track C:** T03 (ADRs, after T02 only)

### Sprint 1 Parallel Tracks
- **Track A (critical):** T01 -> T07 -> T08
- **Track B:** T02, T03 (AWS specialist, feed into T07)
- **Track C:** T04 -> T05 (security, independent chain)
- **Track D:** T09 (designer, fully independent)
- **Track E:** T06 (Docker Compose, after T01 only)

### Sprint 3 Parallel Tracks
- **Track A (critical):** T01 -> T02 -> T04 -> T06
- **Track B:** T01 -> T03 -> T05 (rate limiter -> API)

### Sprint 4 Parallel Tracks
- **Track A (critical):** T01 -> T06?
- **Track B:** T02 -> T03 (SRE)
- **Track C:** T04 -> T05 (performance)

### Sprint 6 Parallel Tracks
- **Track A (critical):** T01 -> T03 -> T04 -> T07
- **Track B:** T02 (security scans, independent)
- **Track C:** T05 -> T06 (DevOps prod, independent)

---

## Gate Checkpoints

```
[S0 GATE] ─── spec-validate PASS, all docs exist
     │
     ▼
[S1 GATE] ─── Docker Compose, 9 TF modules, CI workflow
     │
     ▼
[S2 GATE] ─── 6 .feature files, go build compiles, BDD FAIL (red)
     │
     ▼
[S3 GATE] ─── make bdd GREEN, make test PASS, coverage >= 80%, 3 Lambda zips
     │
     ▼
[S4 GATE] ─── zero BLOCKER findings, dashboards + perf docs exist
     │
     ▼
[S5 GATE] ─── make test-all GREEN, zero blockers, worker + auth wired
     │
     ▼
[FINAL GATE] ─ test-all + check + build PASS, p99 <= 100ms, all P0 covered
```

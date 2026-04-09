# Shorty Decisions Log

All process and architectural decisions made during orchestration. Append-only -- entries are never modified after recording.

---

## DEC-001: Spec-First Development (OpenAPI -> Code Gen)

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Use OpenAPI 3.0 spec as single source of truth; generate Go server stubs and types via oapi-codegen before any implementation |
| Rationale | Spec-first ensures API contract is agreed upon before code is written, preventing drift between documentation and implementation. Generated stubs enforce type safety and reduce boilerplate. The spec also serves as input for BDD feature files, ensuring test coverage matches the API surface. |
| Alternatives Considered | (1) Code-first with Swagger annotations -- rejected because annotations scatter the contract across source files and are easy to forget. (2) Manual API doc maintenance -- rejected because docs inevitably drift from implementation. (3) Protobuf/gRPC -- rejected because the service is HTTP/REST-facing with browser clients. |
| Decided By | Planner (Orchestrator), based on `requirements-init.md` section 11.1 and CLAUDE.md engineering flow |

---

## DEC-002: BDD-First Testing Strategy (Red -> Green)

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Write BDD `.feature` files and E2E test skeletons before any implementation code. Tests must fail (red) first, then implementation makes them pass (green). |
| Rationale | BDD-first ensures that acceptance criteria from the product backlog are encoded as executable specifications before development begins. This prevents "testing what was built" instead of "building what was specified." The red state gate (Sprint 2) provides a checkpoint that tests exist and are meaningful -- they fail because the feature is not implemented, not because the tests are wrong. |
| Alternatives Considered | (1) TDD at unit level only -- rejected because unit tests alone do not verify end-to-end behavior or acceptance criteria. (2) Write tests after implementation -- rejected because post-hoc tests tend to test implementation details rather than requirements. (3) Manual QA only -- rejected because it does not scale and cannot be automated in CI. |
| Decided By | Planner (Orchestrator), based on CLAUDE.md engineering flow and `requirements-init.md` section 11.1 |

---

## DEC-003: Split Implementation Across Sprint 3 (Core) and Sprint 5 (Stats/Auth/Worker)

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Core redirect + API CRUD are implemented in Sprint 3. Stats endpoints, authentication middleware, and SQS worker are deferred to Sprint 5. |
| Rationale | Sprint 3 focuses on the "hot path" -- redirect and link CRUD -- which form the core value loop. These must be solid before adding stats, auth, and async processing. Splitting implementation allows a code review gate (Sprint 4) between the two phases, catching architectural issues before they propagate to secondary features. The worker Lambda depends on the redirect Lambda being stable (it processes events from the redirect path). Auth middleware depends on the API being structurally complete. Stats depend on the worker populating the clicks table. |
| Alternatives Considered | (1) Single implementation sprint -- rejected because a single mega-sprint (12+ tasks) is too large to review effectively, too risky for token exhaustion, and provides no intermediate quality gate. (2) Three implementation sprints (redirect, API, worker+stats) -- considered but rejected as over-splitting; the redirect and API share enough infrastructure (store, cache, ratelimit) that they benefit from being built together. |
| Decided By | Planner (Orchestrator), referencing sprint plan template in `.claude/agents/planner.md` |

---

## DEC-004: Code Review Gate Between Implementation Sprints

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Sprint 4 is a mandatory review + observability sprint between Sprint 3 (core implementation) and Sprint 5 (secondary implementation). No new feature code proceeds until the review gate passes with zero BLOCKER findings. |
| Rationale | A review gate after the largest implementation sprint serves three purposes: (1) Catches security, correctness, and performance issues before they are built upon. (2) Forces the Go developer to address feedback before context switches to stats/auth/worker. (3) Allows SRE and performance engineering to run in parallel with review, maximizing agent utilization. Without this gate, blockers discovered late in Sprint 5 would cascade into Sprint 6 hardening. |
| Alternatives Considered | (1) Review at the end after all implementation -- rejected because late review findings are exponentially more expensive to fix. (2) Continuous review per task -- rejected because individual task review lacks the holistic view needed to catch cross-cutting issues (e.g., consistent error handling, proper context propagation). |
| Decided By | Planner (Orchestrator), based on `.claude/agents/planner.md` sprint template |

---

## DEC-005: Checkpoint-Based Resumability Mechanism

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Use a dual-layer checkpoint system: `plan/progress.json` (machine-readable state) + `plan/checkpoints/{task-id}.done` (atomic completion markers). Tasks write all output files first, then the checkpoint file last. |
| Rationale | Token budgets can exhaust mid-task. The checkpoint mechanism ensures: (1) Idempotent resumption -- if interrupted before checkpoint, the task re-executes. (2) Visual progress -- `ls plan/checkpoints/` shows completed work at a glance. (3) Crash recovery -- checkpoint files can rebuild progress.json if it is corrupted. (4) No read-modify-write races -- each checkpoint is append-only. The "outputs first, checkpoint last" write order ensures that a checkpoint only exists when all outputs are verified. |
| Alternatives Considered | (1) Single progress.json only -- rejected because a corrupted JSON file would lose all state. (2) Git commits as checkpoints -- rejected because commits are heavier-weight and require staged files, mixing checkpoint concerns with version control. (3) External state store (Redis/DynamoDB) -- rejected because the project runs locally and should not depend on infrastructure that has not been built yet. |
| Decided By | Planner (Orchestrator), based on master plan (`cheeky-crafting-wozniak.md`) section 1 |

---

## DEC-006: Redirect Lambda Performance Budget

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | The redirect Lambda (cmd/redirect) has a strict p99 < 100ms target. Click event publishing to SQS is async (goroutine + timeout) and must never block the redirect response. Any change to the redirect critical path requires benchmark validation. |
| Rationale | The redirect endpoint is the highest-traffic, most latency-sensitive path (10,000 RPS target per NFR 3.1). Users expect redirects to feel instant. The async SQS pattern decouples analytics from the redirect hot path -- if SQS is slow or unavailable, the redirect still succeeds. The benchmark requirement prevents performance regressions from creeping in through code review blind spots. |
| Alternatives Considered | (1) Synchronous click recording -- rejected because DynamoDB writes add 5-15ms and SQS publishes add 10-30ms, both unacceptable for the p99 budget. (2) Fire-and-forget without timeout -- rejected because a hung goroutine could leak resources. (3) Batch click events in memory and flush periodically -- rejected because Lambda instances are short-lived and unflushed events would be lost. |
| Decided By | Architect + Planner, based on `requirements-init.md` NFR 3.1 and CLAUDE.md architecture section |

---

## DEC-007: DynamoDB Single-Table Design with Separate Clicks Table

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Use two physical DynamoDB tables (`links` and `clicks`) plus a `users` table, rather than a true single-table design for all entities. The `links` table uses single-table patterns internally (PK=`LINK#{code}`, SK=`META`). |
| Rationale | Click events have fundamentally different access patterns, TTL requirements (90-day), and throughput characteristics than link metadata. Separating them allows independent scaling, independent backup policies, and avoids hot partition issues where a viral link's click writes would throttle metadata reads. The `links` table benefits from single-table patterns (link + future sub-entities share a PK), while clicks benefit from their own table with optimized GSIs for time-series queries. |
| Alternatives Considered | (1) True single-table for everything -- rejected due to hot partition risk and inability to set different TTL policies per entity type. (2) One table per entity (links, clicks, users, plus separate tables for stats aggregates) -- rejected as over-normalized for DynamoDB; the current design handles aggregation via GSI queries. |
| Decided By | Architect + DBA, based on `requirements-init.md` section 5 and `docs/architecture/data-model.md` |

---

## DEC-008: Agent Parallelization Strategy

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | Within each sprint, tasks are organized into parallelization waves. Independent tasks in the same wave can be executed by different agents simultaneously. Tasks within a wave have no dependencies on each other. |
| Rationale | Parallelization reduces total calendar time per sprint. For example, Sprint 0 Wave 1 runs PM and Architect concurrently because they read from the same input (requirements-init.md) but produce independent outputs. Sprint 1 runs AWS specialist, security engineer, and designer concurrently. However, parallelization is constrained by the sequential nature of the Claude Code agent model -- in practice, tasks execute one at a time per session, but the wave structure makes it clear which tasks are safe to reorder or skip ahead to. |
| Alternatives Considered | (1) Fully sequential execution -- rejected because it would double the number of sessions needed. (2) Fully parallel with no waves -- rejected because some tasks genuinely depend on others (e.g., Terraform modules need AWS config docs first). |
| Decided By | Planner (Orchestrator) |

---

## DEC-009: Conflict Resolution Priority Order

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | When agent roles conflict, resolve by applying priorities in order: (1) Security requirements, (2) NFR targets (p99 < 100ms, 99.9% availability, 10K RPS), (3) Cost efficiency, (4) Developer experience. |
| Rationale | Security is non-negotiable -- a vulnerability cannot be traded for performance or convenience. NFR targets are contractual (documented in requirements-init.md) and directly impact user experience. Cost efficiency matters for a serverless architecture where poor design can lead to runaway bills. Developer experience is important but can always be improved incrementally after launch. This ordering was established in the planner agent definition and reflects industry-standard prioritization for production services. |
| Alternatives Considered | (1) Equal weight for all concerns -- rejected because conflicts require a tiebreaker. (2) Performance first -- rejected because a fast but insecure service is worse than a slightly slower secure one. |
| Decided By | Planner (Orchestrator), codified in `.claude/agents/planner.md` conflict resolution section |

---

## DEC-010: Must Have (P0) Boundary for MVP

| Field | Value |
|---|---|
| Date | 2026-04-05 |
| Decision | 22 user stories classified as Must Have (P0) per `docs/product/mvp-scope.md`. All P0 stories must have passing BDD/integration tests before the final gate. Should Have (P1) items are included in the sprint plan but are not launch blockers. Could Have (P2) and Won't Have (P3) are excluded from sprint plan tasks. |
| Rationale | The MVP boundary was set to deliver the complete core loop: create link -> redirect -> track clicks -> manage links. Authentication (email + Google SSO) is P0 because user-specific features (dashboard, stats, quotas) require it. Rate limiting is P0 because anonymous access without limits would allow abuse. Terraform and CI/CD are P0 because the project's spec-first, BDD-first workflow depends on automated quality gates. P1 items like custom aliases, password-protected links, and detailed stats breakdowns add value but have workarounds and can ship shortly after launch. |
| Alternatives Considered | (1) Larger MVP including all P1 items -- rejected because it would add 2-3 more sessions to the critical path. (2) Smaller MVP without stats or auth -- rejected because a URL shortener without analytics or user accounts has limited value proposition. |
| Decided By | PM + Planner, based on `docs/product/mvp-scope.md` |

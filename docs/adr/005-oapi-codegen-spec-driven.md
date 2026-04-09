# ADR-005: oapi-codegen Spec-Driven Development

## Status

Accepted

## Context

Shorty follows a spec-first development workflow where `docs/api/openapi.yaml` is the
single source of truth (RFC-0001). The API surface includes 13+ endpoints across
redirect, CRUD, stats, and user management. We need to ensure that:

1. The Go implementation always matches the API specification.
2. Request/response types are generated, not hand-written (prevents drift).
3. Adding a new endpoint requires updating the spec first, then regenerating code.

The Go ecosystem offers several OpenAPI code generators: `oapi-codegen`, `openapi-generator`
(Java-based), `ogen`, and hand-written approaches.

## Decision

We use **oapi-codegen** (v2) to generate a **strict `ServerInterface`** and all
request/response types from the OpenAPI 3.0 spec. The generated code targets the
**Chi router** (compatible with Lambda via `github.com/awslabs/aws-lambda-go-api-proxy`).

### Configuration

```yaml
# config/oapi-codegen.yaml
package: api
generate:
  chi-server: true
  strict-server: true
  models: true
  embedded-spec: true
output: internal/api/generated/server.gen.go
output-options:
  skip-prune: false
```

### Workflow

1. Developer updates `docs/api/openapi.yaml`.
2. `make spec-validate` -- validates the spec (CI gate, must pass first).
3. `make spec-gen` -- runs `oapi-codegen -config config/oapi-codegen.yaml docs/api/openapi.yaml`.
4. Developer implements the `ServerInterface` methods in `internal/api/handlers.go`.
5. Generated files (`*.gen.go`) are committed to the repo but never hand-edited.

### Strict server interface

The `strict-server` option generates an interface where request parameters are already
parsed and validated:

```go
// Generated interface -- developer implements this
type StrictServerInterface interface {
    CreateLink(ctx context.Context, request CreateLinkRequestObject) (CreateLinkResponseObject, error)
    GetLink(ctx context.Context, request GetLinkRequestObject) (GetLinkResponseObject, error)
    // ...
}
```

This eliminates boilerplate HTTP parsing code and ensures compile-time enforcement
of the API contract.

### Why Chi router

- Lightweight, idiomatic Go HTTP router (stdlib `net/http` compatible).
- Path parameter extraction (`/{code}`) maps directly to OpenAPI path parameters.
- Middleware support for rate limiting, auth, telemetry, CORS.
- Works seamlessly with `aws-lambda-go-api-proxy/chi` for Lambda deployment.

## Consequences

**Positive:**
- API specification and implementation cannot drift -- the compiler enforces it.
- New endpoints require spec changes first, enforcing the spec-driven workflow.
- Generated types provide consistent request validation and response serialization.
- Embedded spec enables serving the OpenAPI document from the API itself.
- BDD tests can validate against the same spec, ensuring test coverage matches the
  contract.

**Negative:**
- oapi-codegen v2 has occasional edge cases with complex OpenAPI constructs
  (discriminators, deeply nested oneOf). We keep our schemas straightforward.
- Generated code adds ~2000-4000 lines to the repo. Git diffs on regeneration can
  be noisy.
- Developers must learn the oapi-codegen strict server pattern, which differs from
  typical hand-written Go HTTP handlers.

## Alternatives Considered

**openapi-generator (Java-based):**
Rejected. Requires JVM in the CI environment, generates verbose Go code with deep
package hierarchies, and the Go templates are community-maintained with inconsistent
quality.

**ogen (Go-native OpenAPI generator):**
Considered. ogen generates more idiomatic Go code and supports OpenAPI 3.1. However,
it is less mature than oapi-codegen, has a smaller community, and its Chi integration
is experimental. We may revisit when ogen reaches 1.0.

**Hand-written handlers (no code generation):**
Rejected. With 13+ endpoints, hand-written request parsing is error-prone and creates
drift risk between the spec and implementation. The spec-first workflow requires
mechanical enforcement.

**gRPC + grpc-gateway:**
Rejected. Adds complexity (proto files + OpenAPI spec = two sources of truth), requires
gRPC toolchain in CI, and the URL shortener API is naturally RESTful. gRPC is better
suited for internal service-to-service communication.

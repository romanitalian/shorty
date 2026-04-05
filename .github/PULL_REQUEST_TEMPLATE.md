## Summary

<!-- One paragraph: what changed and why. -->

Closes #<!-- issue number -->

## Type of Change

<!-- Check all that apply -->
- [ ] `feat` — new feature
- [ ] `fix` — bug fix
- [ ] `perf` — performance improvement
- [ ] `refactor` — code change with no functional effect
- [ ] `test` — adding or updating tests
- [ ] `docs` — documentation only
- [ ] `infra` — Terraform / CI / Docker changes
- [ ] `spec` — OpenAPI spec change (requires `make spec-gen` after merge)

## Breaking Changes

- [ ] This PR introduces a breaking change (API, data model, or config)

<!-- If yes, describe the migration path: -->

## How to Test

<!-- Steps for the reviewer to verify the change manually if needed. -->

```bash
make dev-up
# describe what to do
```

## Checklist

- [ ] `make spec-validate` passes (if OpenAPI was changed)
- [ ] `make bdd` — all BDD scenarios green
- [ ] `make test` — unit tests pass with `-race`
- [ ] `make check` — fmt + vet + lint + gosec clean
- [ ] BDD `.feature` files updated / added (if behaviour changed)
- [ ] ADR added to `docs/adr/` (if architectural decision was made)
- [ ] Performance impact considered (if redirect critical path was touched)
- [ ] No secrets, tokens, or PII in code or logs

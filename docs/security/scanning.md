# Security Scanning Configuration -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T05)
**References:** security-architecture.md, threat-model.md

---

## 1. Scanning Pipeline Overview

```
Developer Push / PR
  |
  v
GitHub Actions CI Pipeline
  |
  +-- Stage: lint (make check)
  |
  +-- Stage: test (make test)
  |
  +-- Stage: security (GATE -- blocks merge on failure)
  |     |
  |     +-- gosec (SAST -- Go security linter)
  |     +-- govulncheck (dependency vulnerability scan)
  |     +-- gitleaks (secret detection in code/history)
  |     +-- trivy (dependency + container scan)
  |     +-- [upload SARIF to GitHub Code Scanning]
  |
  +-- Stage: build (make build)
  |
  +-- Stage: deploy-dev (make deploy-dev)
  |     |
  |     +-- OWASP ZAP baseline scan (DAST -- post-deploy)
  |
  +-- Stage: bdd + e2e tests
```

---

## 2. gosec -- Go Static Security Analysis (SAST)

### 2.1 Purpose

gosec inspects Go source code for security issues: hardcoded credentials, SQL/NoSQL injection patterns, weak crypto, SSRF, unhandled errors, and more.

### 2.2 Installation

```bash
# In CI (GitHub Actions)
go install github.com/securego/gosec/v2/cmd/gosec@latest

# Local development
brew install gosec  # macOS
# or
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### 2.3 Configuration

Create `.gosec.yaml` in the project root:

```yaml
# .gosec.yaml -- gosec configuration for Shorty

global:
  # Fail on medium severity and above
  severity: medium
  confidence: medium
  # Exclude test files from scanning
  exclude-dir:
    - vendor
    - tests/bdd

# Rules to explicitly enable (all security-relevant for Shorty)
rules:
  include:
    # Credentials
    - G101  # Look for hard-coded credentials
    # Injection
    - G201  # SQL query construction using format string
    - G202  # SQL query construction using string concatenation
    - G203  # Use of unescaped data in HTML templates
    - G204  # Audit use of command execution
    # Crypto
    - G401  # Detect use of DES, RC4, MD5, or SHA1
    - G402  # Look for bad TLS connection settings
    - G403  # Ensure minimum RSA key length of 2048 bits
    - G404  # Insecure random number source (math/rand)
    # Error handling
    - G104  # Audit errors not checked
    # File/path
    - G301  # Poor file permissions on directory creation
    - G302  # Poor file permissions on file creation
    - G304  # File path provided as taint input (path traversal)
    - G305  # File traversal when extracting zip/tar archive
    # Network
    - G107  # URL provided to HTTP request as taint input (SSRF)
    - G108  # Profiling endpoint automatically exposed to /debug/pprof
    - G109  # Potential Integer overflow in strconv
    - G110  # Potential DoS via decompression bomb
    - G114  # Use of net/http serve function with no timeout
    # Crypto
    - G501  # Import blocklist: crypto/md5
    - G502  # Import blocklist: crypto/des
    - G503  # Import blocklist: crypto/rc4
    - G504  # Import blocklist: net/http/cgi
    - G505  # Import blocklist: crypto/sha1
```

### 2.4 CLI Commands

```bash
# Full scan with SARIF output (for GitHub Code Scanning)
gosec -fmt=sarif -out=gosec.sarif -severity=medium -confidence=medium ./...

# Full scan with console output (local development)
gosec -fmt=text ./...

# Scan specific package
gosec -fmt=text ./internal/store/...

# Scan with JSON output (for CI parsing)
gosec -fmt=json -out=gosec.json ./...

# Exclude specific rules for a file (use inline comments)
# //nolint:gosec // G404: math/rand used for non-security purpose (test data)
```

### 2.5 Critical Rules for Shorty

| Rule | Why It Matters | Shorty Context |
|---|---|---|
| G107 | SSRF detection | Flags any user-controlled URL passed to `http.Get()`. Our URL validation pipeline (security-architecture.md Section 2) must run before any HTTP request. |
| G401 | Weak hash algorithms | IP hashing must use SHA-256, never MD5 or SHA-1. |
| G501/G505 | Weak crypto imports | Block `crypto/md5` and `crypto/sha1` imports. |
| G104 | Unhandled errors | DynamoDB and Redis calls must handle errors (fail-closed for rate limiter). |
| G304 | Path traversal | Custom alias injection (`../../../etc/passwd`) detection. |
| G404 | Insecure random | Code generation (shortener) must use `crypto/rand`, not `math/rand`. |
| G114 | HTTP server timeout | All HTTP clients must have timeouts (JWKS fetch, Safe Browsing API). |

### 2.6 Expected Findings to Suppress

Some findings are expected and documented with suppression comments:

```go
// In test files -- math/rand for test data generation is acceptable
//nolint:gosec // G404: math/rand used for non-cryptographic test data generation

// In URL validator -- we intentionally parse untrusted URLs
//nolint:gosec // G107: URL is validated by our pipeline before any HTTP request is made
```

---

## 3. govulncheck -- Go Vulnerability Scanner

### 3.1 Purpose

govulncheck uses call graph analysis to find known vulnerabilities in Go dependencies that are actually reachable from application code. This produces fewer false positives than module-level scanning.

### 3.2 Installation

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

### 3.3 CLI Commands

```bash
# Scan all packages (default: call graph analysis)
govulncheck ./...

# Scan with JSON output (for CI parsing)
govulncheck -json ./... > govulncheck.json

# Scan specific binary
govulncheck -mode=binary ./bin/shorty-redirect

# Check a specific module
govulncheck -C ./cmd/redirect ./...
```

### 3.4 CI Fail Criteria

- **Any vulnerability found:** Pipeline fails. govulncheck only reports vulnerabilities in reachable code paths, so every finding is actionable.
- **Resolution options:**
  1. Upgrade the affected dependency: `go get -u module@version`
  2. If no fix is available, document in `docs/security/accepted-risks.md` with:
     - Vulnerability ID (GO-YYYY-NNNN)
     - Affected code path
     - Mitigation (if not upgradeable)
     - Acceptance date and reviewer

### 3.5 Example Output

```
Scanning your code and 127 packages across 45 deps for known vulnerabilities...

Vulnerability #1: GO-2024-1234
  stdlib: net/http: improper handling of chunked encoding
  Found in: net/http@go1.22.0
  Fixed in: net/http@go1.22.1
  Affected code:
    cmd/api/main.go:42: http.ListenAndServe
```

---

## 4. Trivy -- Container and Dependency Scanner

### 4.1 Purpose

Trivy scans container images, filesystem dependencies (go.sum), and IaC (Terraform) for vulnerabilities, misconfigurations, and exposed secrets.

### 4.2 Installation

```bash
# macOS
brew install trivy

# CI (GitHub Actions) -- use official action
# aquasecurity/trivy-action@master
```

### 4.3 CLI Commands

```bash
# Filesystem scan (go.sum, package-lock.json, Dockerfile)
trivy fs . --severity HIGH,CRITICAL --exit-code 1

# Container image scan (after build)
trivy image shorty-redirect:latest --severity HIGH,CRITICAL --exit-code 1

# Dockerfile scan (before build)
trivy config Dockerfile.dev --severity HIGH,CRITICAL --exit-code 1

# Terraform IaC scan
trivy config deploy/terraform/ --severity HIGH,CRITICAL --exit-code 1

# Full scan with SARIF output
trivy fs . --severity HIGH,CRITICAL --format sarif --output trivy.sarif

# JSON output for CI parsing
trivy fs . --severity HIGH,CRITICAL --format json --output trivy.json
```

### 4.4 Trivy Configuration

Create `.trivyignore` in the project root for accepted risks:

```
# .trivyignore -- Trivy ignore list for Shorty
# Format: vulnerability ID + comment

# Example: CVE accepted with justification
# CVE-2024-XXXXX  # accepted: not reachable in our code path (see docs/security/accepted-risks.md)
```

### 4.5 CI Configuration

```yaml
# Trivy filesystem scan
- name: Trivy filesystem scan
  run: |
    trivy fs . \
      --severity HIGH,CRITICAL \
      --exit-code 1 \
      --format sarif \
      --output trivy-fs.sarif

# Trivy IaC scan (Terraform)
- name: Trivy IaC scan
  run: |
    trivy config deploy/terraform/ \
      --severity HIGH,CRITICAL \
      --exit-code 1 \
      --format sarif \
      --output trivy-iac.sarif
```

---

## 5. OWASP ZAP -- Dynamic Application Security Testing (DAST)

### 5.1 Purpose

ZAP performs active and passive security testing against a running instance of Shorty. It detects XSS, injection, CSRF issues, insecure headers, and other runtime vulnerabilities.

### 5.2 Baseline Scan (CI -- Passive)

The baseline scan performs passive analysis only (no active attacks). Safe for CI.

```bash
# Run against local stack
docker run --rm \
  --network host \
  -v $(pwd)/zap-config:/zap/wrk:rw \
  ghcr.io/zaproxy/zaproxy:stable \
  zap-baseline.py \
    -t http://localhost:8080 \
    -r zap-report.html \
    -J zap-report.json \
    -c zap-baseline.conf \
    -I  # informational alerts do not fail the scan
```

### 5.3 ZAP Baseline Configuration

Create `zap-config/zap-baseline.conf`:

```ini
# zap-baseline.conf -- OWASP ZAP baseline scan configuration for Shorty

# Maximum scan duration (seconds)
10020 = WARN

# Content-Security-Policy header missing
10038 = FAIL

# X-Frame-Options header missing
10020 = FAIL

# X-Content-Type-Options header missing
10021 = FAIL

# Strict-Transport-Security header missing
10035 = FAIL

# Cookie without HttpOnly flag
10010 = FAIL

# Cookie without Secure flag
10011 = FAIL

# Cookie without SameSite attribute
10054 = WARN

# Cross-Site Scripting (reflected)
40012 = FAIL

# Cross-Site Scripting (persistent)
40014 = FAIL

# SQL Injection
40018 = FAIL

# Remote File Inclusion
7 = FAIL

# Server-side Include
40009 = WARN

# Cross-Domain Misconfiguration
10098 = WARN

# Information Disclosure - Debug Errors
10023 = FAIL

# Information Disclosure - Sensitive data in URL
10024 = FAIL

# Absence of Anti-CSRF Tokens (forms only)
10202 = WARN
```

### 5.4 Full Scan (Weekly -- Active)

Run a full active scan weekly against the dev environment:

```bash
docker run --rm \
  -v $(pwd)/zap-config:/zap/wrk:rw \
  ghcr.io/zaproxy/zaproxy:stable \
  zap-full-scan.py \
    -t https://dev.shorty.io \
    -r zap-full-report.html \
    -J zap-full-report.json \
    -a  # include alpha passive scan rules
```

### 5.5 Target URLs for Scanning

| Target | URL | Auth Required | Notes |
|---|---|---|---|
| Redirect endpoint | `GET http://localhost:8080/{code}` | No | Test with valid and invalid codes |
| Guest create | `POST http://localhost:8080/api/v1/shorten` | No | Test input validation |
| Password form | `GET http://localhost:8080/{code}` (password-protected) | No | Test CSRF, XSS |
| Password submit | `POST http://localhost:8080/p/{code}` | No | Test brute-force, CSRF |
| Auth CRUD | `POST http://localhost:8080/api/v1/links` | JWT | Test injection, authz |
| Stats | `GET http://localhost:8080/api/v1/links/{code}/stats` | JWT | Test authz bypass |

### 5.6 Alert Threshold

| Severity | CI Gate | Action |
|---|---|---|
| HIGH | Fail pipeline | Must fix before merge |
| MEDIUM | Warn (non-blocking) | Fix within current sprint |
| LOW | Info only | Track in backlog |
| INFORMATIONAL | Ignore | No action |

---

## 6. gitleaks -- Secret Detection

### 6.1 Purpose

gitleaks scans git history and staged files for accidentally committed secrets (API keys, passwords, tokens, private keys).

### 6.2 Installation

```bash
# macOS
brew install gitleaks

# CI (GitHub Actions)
# zricethezav/gitleaks-action@v2
```

### 6.3 Configuration

Create `.gitleaks.toml` in the project root:

```toml
# .gitleaks.toml -- gitleaks configuration for Shorty

title = "Shorty Secret Detection"

[extend]
# Use the default gitleaks rules as a base
useDefault = true

# Custom rules for Shorty-specific patterns
[[rules]]
id = "shorty-ip-salt"
description = "Shorty IP hash salt"
regex = '''(?i)(ip[_-]?hash[_-]?salt|IP_HASH_SALT)\s*[=:]\s*['"]?[a-f0-9]{32,64}['"]?'''
tags = ["key", "shorty"]

[[rules]]
id = "shorty-csrf-key"
description = "Shorty CSRF key"
regex = '''(?i)(csrf[_-]?key|CSRF_KEY)\s*[=:]\s*['"]?[a-f0-9]{32,128}['"]?'''
tags = ["key", "shorty"]

[[rules]]
id = "shorty-redis-auth"
description = "Shorty Redis AUTH token"
regex = '''(?i)(redis[_-]?auth|REDIS_AUTH_TOKEN)\s*[=:]\s*['"]?[a-zA-Z0-9]{32,128}['"]?'''
tags = ["key", "shorty"]

# Allow patterns (not secrets)
[allowlist]
description = "Global allowlist"
paths = [
  '''\.gitleaks\.toml$''',
  '''\.env\.example$''',
  '''docs/security/.*\.md$''',
  '''tests/.*_test\.go$''',
]

# Specific string allowlist for known false positives
regexes = [
  '''change-me-.*''',
  '''local-dev-salt-not-for-production''',
  '''local-redis-token''',
  '''CHANGE_ME''',
]
```

### 6.4 CLI Commands

```bash
# Scan current state of the repo
gitleaks detect --source . --verbose

# Scan git history (all commits)
gitleaks detect --source . --log-opts="--all" --verbose

# Scan staged files only (pre-commit hook)
gitleaks protect --staged --verbose

# JSON output for CI
gitleaks detect --source . --report-format json --report-path gitleaks.json

# SARIF output for GitHub Code Scanning
gitleaks detect --source . --report-format sarif --report-path gitleaks.sarif
```

### 6.5 Pre-Commit Hook Integration

Add to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/gitleaks/gitleaks
    rev: v8.18.0
    hooks:
      - id: gitleaks
```

Or manual git hook (`.git/hooks/pre-commit`):

```bash
#!/bin/bash
# .git/hooks/pre-commit -- gitleaks secret detection

if command -v gitleaks &> /dev/null; then
    gitleaks protect --staged --verbose --redact
    if [ $? -ne 0 ]; then
        echo ""
        echo "ERROR: gitleaks detected secrets in staged files."
        echo "Remove the secrets and try again."
        echo "If this is a false positive, add to .gitleaks.toml allowlist."
        exit 1
    fi
fi
```

---

## 7. GitHub Actions Integration

### 7.1 Security Stage in CI Workflow

```yaml
# .github/workflows/ci.yml

name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  security:
    name: Security Scanning
    runs-on: ubuntu-latest
    permissions:
      security-events: write  # required for SARIF upload
      contents: read
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0  # full history for gitleaks

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      # --- gosec ---
      - name: Install gosec
        run: go install github.com/securego/gosec/v2/cmd/gosec@latest

      - name: Run gosec
        run: gosec -fmt=sarif -out=gosec.sarif -severity=medium -confidence=medium ./...

      - name: Upload gosec SARIF
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: gosec.sarif
          category: gosec
        if: always()

      # --- govulncheck ---
      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        run: govulncheck ./...

      # --- gitleaks ---
      - name: Run gitleaks
        uses: gitleaks/gitleaks-action@v2
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      # --- trivy (filesystem) ---
      - name: Run Trivy filesystem scan
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: fs
          scan-ref: .
          severity: HIGH,CRITICAL
          exit-code: 1
          format: sarif
          output: trivy-fs.sarif

      - name: Upload Trivy SARIF
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: trivy-fs.sarif
          category: trivy
        if: always()

      # --- trivy (IaC) ---
      - name: Run Trivy IaC scan
        uses: aquasecurity/trivy-action@master
        with:
          scan-type: config
          scan-ref: deploy/terraform/
          severity: HIGH,CRITICAL
          exit-code: 1
        continue-on-error: true  # IaC scan is advisory during early sprints

  # DAST runs after deployment to dev
  dast:
    name: OWASP ZAP Baseline
    needs: [security, build, deploy-dev]
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v4

      - name: ZAP Baseline Scan
        uses: zaproxy/action-baseline@v0.12.0
        with:
          target: https://dev.shorty.io
          rules_file_name: zap-config/zap-baseline.conf
          fail_action: true
          allow_issue_writing: false

      - name: Upload ZAP Report
        uses: actions/upload-artifact@v4
        with:
          name: zap-report
          path: zap-report.html
        if: always()
```

### 7.2 Makefile Targets

```makefile
# Makefile additions for security scanning

.PHONY: security gosec govulncheck gitleaks trivy

security: gosec govulncheck gitleaks trivy  ## Run all security scans

gosec:  ## Run Go security linter
	gosec -fmt=text -severity=medium -confidence=medium ./...

govulncheck:  ## Run Go vulnerability checker
	govulncheck ./...

gitleaks:  ## Scan for secrets in git history
	gitleaks detect --source . --verbose

trivy:  ## Run Trivy filesystem scan
	trivy fs . --severity HIGH,CRITICAL --exit-code 1

trivy-iac:  ## Run Trivy IaC scan on Terraform
	trivy config deploy/terraform/ --severity HIGH,CRITICAL

zap-baseline:  ## Run ZAP baseline scan against local stack
	docker run --rm --network host \
		-v $(PWD)/zap-config:/zap/wrk:rw \
		ghcr.io/zaproxy/zaproxy:stable \
		zap-baseline.py -t http://localhost:8080 -r zap-report.html -I
```

---

## 8. Scan Schedule Summary

| Scanner | Trigger | Gate Type | Severity Threshold |
|---|---|---|---|
| gosec | Every PR, every push to main | Blocking | Medium+ |
| govulncheck | Every PR, every push to main | Blocking | Any finding |
| gitleaks | Every PR, pre-commit hook | Blocking | Any finding |
| Trivy (filesystem) | Every PR, every push to main | Blocking | HIGH/CRITICAL |
| Trivy (IaC) | Every PR, every push to main | Advisory (Sprint 0-2), Blocking (Sprint 3+) |  HIGH/CRITICAL |
| OWASP ZAP (baseline) | After deploy to dev | Blocking on HIGH | HIGH |
| OWASP ZAP (full) | Weekly cron (Saturday) | Advisory | MEDIUM+ |

---

## 9. Handling Findings

### 9.1 Workflow

1. Scanner reports finding in CI
2. Developer reviews finding:
   - **True positive:** Fix immediately (HIGH/CRITICAL) or within sprint (MEDIUM)
   - **False positive:** Add suppression with documented justification
   - **Accepted risk:** Document in `docs/security/accepted-risks.md` with date, rationale, and reviewer approval
3. All suppressions and accepted risks are reviewed quarterly by the Security Engineer

### 9.2 Suppression Documentation

```go
// Example: gosec suppression with justification
//nolint:gosec // G404: math/rand is used only for jitter in exponential backoff,
// not for security-sensitive operations. crypto/rand is used for all
// cryptographic purposes (code generation, salt generation).
jitter := time.Duration(rand.Intn(100)) * time.Millisecond
```

```toml
# Example: gitleaks allowlist with justification
# .gitleaks.toml
[[rules.allowlist]]
description = "Test fixtures use dummy secrets"
regexes = ['''test-secret-.*''']
```

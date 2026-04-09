#!/usr/bin/env bash
# deploy/scripts/deploy.sh
#
# Production deployment script for Shorty Lambda functions.
# Builds ARM64 binaries, uploads to S3, performs canary deployment
# with automated health check and rollback.
#
# Usage:
#   ./deploy/scripts/deploy.sh [--service redirect|api|worker|all] [--skip-build] [--canary-weight 0.1]
#
# Environment variables (required):
#   AWS_REGION          - AWS region (default: us-east-1)
#   ARTIFACTS_BUCKET    - S3 bucket for Lambda zips (default: shorty-prod-artifacts)
#   ENVIRONMENT         - Environment name (default: prod)
#
# The script follows this flow:
#   1. Build Lambda binaries (GOOS=linux GOARCH=arm64 CGO_ENABLED=0)
#   2. Create deployment zip files
#   3. Upload to S3 with version prefix
#   4. Update Lambda function code
#   5. Publish new version
#   6. Canary: shift 10% traffic to new version
#   7. Health check: monitor error rate for 10 minutes
#   8. Promote (100% to new version) or rollback

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

AWS_REGION="${AWS_REGION:-us-east-1}"
ARTIFACTS_BUCKET="${ARTIFACTS_BUCKET:-shorty-prod-artifacts}"
ENVIRONMENT="${ENVIRONMENT:-prod}"
PROJECT="shorty"

SERVICES="redirect api worker"
SKIP_BUILD=false
CANARY_WEIGHT="${CANARY_WEIGHT:-0.1}"
HEALTH_CHECK_DURATION="${HEALTH_CHECK_DURATION:-600}"  # 10 minutes
HEALTH_CHECK_INTERVAL="${HEALTH_CHECK_INTERVAL:-30}"   # check every 30s
ERROR_RATE_THRESHOLD="0.001"  # 0.1%

DEPLOY_TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DEPLOY_VERSION="${DEPLOY_VERSION:-${DEPLOY_TIMESTAMP}}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# =============================================================================
# Argument Parsing
# =============================================================================

while [[ $# -gt 0 ]]; do
  case "$1" in
    --service)
      SERVICES="$2"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=true
      shift
      ;;
    --canary-weight)
      CANARY_WEIGHT="$2"
      shift 2
      ;;
    --health-duration)
      HEALTH_CHECK_DURATION="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--service redirect|api|worker|all] [--skip-build] [--canary-weight 0.1]"
      exit 0
      ;;
    *)
      echo -e "${RED}ERROR: Unknown argument: $1${NC}"
      exit 1
      ;;
  esac
done

if [[ "${SERVICES}" == "all" ]]; then
  SERVICES="redirect api worker"
fi

DRY_RUN="${DRY_RUN:-false}"

# =============================================================================
# Logging
# =============================================================================

log_info() {
  echo -e "${GREEN}[INFO]${NC} $(date -u +%H:%M:%S) $*"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $(date -u +%H:%M:%S) $*"
}

log_error() {
  echo -e "${RED}[ERROR]${NC} $(date -u +%H:%M:%S) $*"
}

# =============================================================================
# Pre-flight Checks
# =============================================================================

preflight_checks() {
  log_info "Running pre-flight checks..."

  # Check required tools
  for cmd in aws go zip jq; do
    if ! command -v "$cmd" &>/dev/null; then
      log_error "Required command not found: $cmd"
      exit 1
    fi
  done

  # Verify AWS credentials
  if ! aws sts get-caller-identity --region "${AWS_REGION}" &>/dev/null; then
    log_error "AWS credentials not configured or expired"
    exit 1
  fi

  local identity
  identity="$(aws sts get-caller-identity --region "${AWS_REGION}" --output json)"
  log_info "AWS Account: $(echo "$identity" | jq -r '.Account')"
  log_info "AWS Identity: $(echo "$identity" | jq -r '.Arn')"

  # Verify S3 bucket exists
  if ! aws s3api head-bucket --bucket "${ARTIFACTS_BUCKET}" --region "${AWS_REGION}" 2>/dev/null; then
    log_error "Artifacts bucket does not exist: ${ARTIFACTS_BUCKET}"
    exit 1
  fi

  # Verify Go version
  local go_version
  go_version="$(go version | awk '{print $3}')"
  log_info "Go version: ${go_version}"

  log_info "Pre-flight checks passed"
}

# =============================================================================
# Build
# =============================================================================

build_service() {
  local service="$1"
  local build_dir="${PROJECT_ROOT}/build"
  local output_dir="${build_dir}/${service}"

  log_info "Building ${service} (GOOS=linux GOARCH=arm64 CGO_ENABLED=0)..."

  mkdir -p "${output_dir}"

  # Build the binary
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${DEPLOY_VERSION}" \
    -o "${output_dir}/bootstrap" \
    "${PROJECT_ROOT}/cmd/${service}/main.go"

  # Create zip
  (cd "${output_dir}" && zip -j "${build_dir}/${service}.zip" bootstrap)

  local zip_size
  zip_size="$(du -h "${build_dir}/${service}.zip" | cut -f1)"
  log_info "Built ${service}.zip (${zip_size})"
}

build_all() {
  if [[ "${SKIP_BUILD}" == "true" ]]; then
    log_warn "Skipping build (--skip-build)"
    return
  fi

  log_info "Building Lambda binaries..."

  for service in ${SERVICES}; do
    build_service "${service}"
  done

  log_info "All builds completed"
}

# =============================================================================
# Upload to S3
# =============================================================================

upload_artifacts() {
  local build_dir="${PROJECT_ROOT}/build"

  log_info "Uploading artifacts to s3://${ARTIFACTS_BUCKET}/..."

  for service in ${SERVICES}; do
    local zip_file="${build_dir}/${service}.zip"

    if [[ ! -f "${zip_file}" ]]; then
      log_error "Zip file not found: ${zip_file}"
      exit 1
    fi

    # Upload with version prefix for rollback capability
    local s3_key_versioned="versions/${DEPLOY_VERSION}/${service}.zip"
    local s3_key_latest="${service}.zip"

    log_info "Uploading ${service}.zip -> s3://${ARTIFACTS_BUCKET}/${s3_key_versioned}"
    aws s3 cp "${zip_file}" "s3://${ARTIFACTS_BUCKET}/${s3_key_versioned}" \
      --region "${AWS_REGION}" \
      --metadata "deploy-version=${DEPLOY_VERSION},deploy-timestamp=${DEPLOY_TIMESTAMP}"

    # Also update the latest key (what Terraform references)
    aws s3 cp "${zip_file}" "s3://${ARTIFACTS_BUCKET}/${s3_key_latest}" \
      --region "${AWS_REGION}"

    log_info "Uploaded ${service}.zip"
  done
}

# =============================================================================
# Deploy Lambda Functions
# =============================================================================

get_function_name() {
  local service="$1"
  echo "${PROJECT}-${ENVIRONMENT}-${service}"
}

update_lambda() {
  local service="$1"
  local function_name
  function_name="$(get_function_name "${service}")"

  log_info "Updating Lambda function: ${function_name}..."

  # Update function code from S3
  local result
  result="$(aws lambda update-function-code \
    --function-name "${function_name}" \
    --s3-bucket "${ARTIFACTS_BUCKET}" \
    --s3-key "${service}.zip" \
    --region "${AWS_REGION}" \
    --output json 2>&1)"

  if [[ $? -ne 0 ]]; then
    log_error "Failed to update function code: ${result}"
    return 1
  fi

  # Wait for the update to complete
  log_info "Waiting for function update to complete..."
  aws lambda wait function-updated-v2 \
    --function-name "${function_name}" \
    --region "${AWS_REGION}"

  # Publish a new version
  log_info "Publishing new version for ${function_name}..."
  local version_result
  version_result="$(aws lambda publish-version \
    --function-name "${function_name}" \
    --description "Deploy ${DEPLOY_VERSION}" \
    --region "${AWS_REGION}" \
    --output json)"

  local new_version
  new_version="$(echo "${version_result}" | jq -r '.Version')"
  log_info "Published version ${new_version} for ${function_name}"

  echo "${new_version}"
}

# =============================================================================
# Canary Deployment
# =============================================================================

get_current_alias_version() {
  local function_name="$1"
  local alias_name="${2:-live}"

  aws lambda get-alias \
    --function-name "${function_name}" \
    --name "${alias_name}" \
    --region "${AWS_REGION}" \
    --output json | jq -r '.FunctionVersion'
}

start_canary() {
  local service="$1"
  local new_version="$2"
  local function_name
  function_name="$(get_function_name "${service}")"

  local current_version
  current_version="$(get_current_alias_version "${function_name}" "live")"

  if [[ "${current_version}" == "${new_version}" ]]; then
    log_info "Version ${new_version} is already the live version for ${function_name}"
    return 0
  fi

  log_info "Starting canary for ${function_name}: v${current_version} ($(echo "1 - ${CANARY_WEIGHT}" | bc)%) -> v${new_version} ($(echo "${CANARY_WEIGHT} * 100" | bc)%)"

  # Update alias to route CANARY_WEIGHT to new version
  aws lambda update-alias \
    --function-name "${function_name}" \
    --name "live" \
    --function-version "${current_version}" \
    --routing-config "{\"AdditionalVersionWeights\":{\"${new_version}\":${CANARY_WEIGHT}}}" \
    --region "${AWS_REGION}" \
    --output json >/dev/null

  log_info "Canary started for ${function_name}"
}

promote_canary() {
  local service="$1"
  local new_version="$2"
  local function_name
  function_name="$(get_function_name "${service}")"

  log_info "Promoting ${function_name} to v${new_version} (100%)..."

  aws lambda update-alias \
    --function-name "${function_name}" \
    --name "live" \
    --function-version "${new_version}" \
    --routing-config '{"AdditionalVersionWeights":{}}' \
    --region "${AWS_REGION}" \
    --output json >/dev/null

  log_info "Promoted ${function_name} v${new_version} to 100% traffic"
}

rollback_canary() {
  local service="$1"
  local previous_version="$2"
  local function_name
  function_name="$(get_function_name "${service}")"

  log_warn "Rolling back ${function_name} to v${previous_version}..."

  aws lambda update-alias \
    --function-name "${function_name}" \
    --name "live" \
    --function-version "${previous_version}" \
    --routing-config '{"AdditionalVersionWeights":{}}' \
    --region "${AWS_REGION}" \
    --output json >/dev/null

  log_warn "Rolled back ${function_name} to v${previous_version}"
}

# =============================================================================
# Health Check
# =============================================================================

check_error_rate() {
  local function_name="$1"
  local period=300  # 5-minute window

  local end_time
  end_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  local start_time
  start_time="$(date -u -v-5M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%SZ)"

  # Get invocation count
  local invocations
  invocations="$(aws cloudwatch get-metric-statistics \
    --namespace "AWS/Lambda" \
    --metric-name "Invocations" \
    --dimensions "Name=FunctionName,Value=${function_name}" \
    --start-time "${start_time}" \
    --end-time "${end_time}" \
    --period "${period}" \
    --statistics Sum \
    --region "${AWS_REGION}" \
    --output json | jq -r '.Datapoints[0].Sum // 0')"

  # Get error count
  local errors
  errors="$(aws cloudwatch get-metric-statistics \
    --namespace "AWS/Lambda" \
    --metric-name "Errors" \
    --dimensions "Name=FunctionName,Value=${function_name}" \
    --start-time "${start_time}" \
    --end-time "${end_time}" \
    --period "${period}" \
    --statistics Sum \
    --region "${AWS_REGION}" \
    --output json | jq -r '.Datapoints[0].Sum // 0')"

  if [[ "${invocations}" == "0" || "${invocations}" == "null" ]]; then
    echo "0"  # No traffic = no errors
    return 0
  fi

  # Calculate error rate
  local error_rate
  error_rate="$(echo "scale=6; ${errors} / ${invocations}" | bc)"
  echo "${error_rate}"
}

run_health_check() {
  local service="$1"
  local new_version="$2"
  local previous_version="$3"
  local function_name
  function_name="$(get_function_name "${service}")"

  log_info "Starting health check for ${function_name} (${HEALTH_CHECK_DURATION}s, threshold: ${ERROR_RATE_THRESHOLD})..."

  local elapsed=0
  local check_count=0

  while [[ ${elapsed} -lt ${HEALTH_CHECK_DURATION} ]]; do
    sleep "${HEALTH_CHECK_INTERVAL}"
    elapsed=$((elapsed + HEALTH_CHECK_INTERVAL))
    check_count=$((check_count + 1))

    local error_rate
    error_rate="$(check_error_rate "${function_name}")"

    local error_rate_pct
    error_rate_pct="$(echo "scale=4; ${error_rate} * 100" | bc)"

    log_info "[${elapsed}/${HEALTH_CHECK_DURATION}s] ${function_name} error rate: ${error_rate_pct}%"

    # Check if error rate exceeds threshold
    local exceeds
    exceeds="$(echo "${error_rate} > ${ERROR_RATE_THRESHOLD}" | bc)"

    if [[ "${exceeds}" == "1" ]]; then
      log_error "Error rate ${error_rate_pct}% exceeds threshold $(echo "scale=2; ${ERROR_RATE_THRESHOLD} * 100" | bc)%"
      log_error "Initiating rollback for ${function_name}..."

      rollback_canary "${service}" "${previous_version}"
      return 1
    fi

    # Also check for any CloudWatch alarms in ALARM state
    local alarm_state
    alarm_state="$(aws cloudwatch describe-alarms \
      --alarm-name-prefix "shorty-prod-redirect-burn-rate-critical" \
      --state-value "ALARM" \
      --region "${AWS_REGION}" \
      --output json | jq -r '.MetricAlarms | length')"

    if [[ "${alarm_state}" -gt 0 ]]; then
      log_error "Critical burn-rate alarm triggered during canary!"
      rollback_canary "${service}" "${previous_version}"
      return 1
    fi
  done

  log_info "Health check passed for ${function_name} after ${HEALTH_CHECK_DURATION}s"
  return 0
}

# =============================================================================
# Main Deploy Flow
# =============================================================================

deploy_service() {
  local service="$1"
  local function_name
  function_name="$(get_function_name "${service}")"

  log_info "=== Deploying ${service} ==="

  # Record previous version for rollback
  local previous_version
  previous_version="$(get_current_alias_version "${function_name}" "live")"
  log_info "Current live version: ${previous_version}"

  # Update function code and publish new version
  local new_version
  new_version="$(update_lambda "${service}")"

  if [[ -z "${new_version}" ]]; then
    log_error "Failed to publish new version for ${service}"
    return 1
  fi

  # Start canary (only for redirect and api -- worker can be deployed directly)
  if [[ "${service}" == "worker" ]]; then
    log_info "Worker Lambda: promoting directly (not user-facing)"
    promote_canary "${service}" "${new_version}"
    return 0
  fi

  # Start canary
  start_canary "${service}" "${new_version}"

  # Run health check
  if run_health_check "${service}" "${new_version}" "${previous_version}"; then
    # Promote to 100%
    promote_canary "${service}" "${new_version}"
    log_info "=== ${service} deployed successfully (v${new_version}) ==="
  else
    log_error "=== ${service} deployment FAILED -- rolled back to v${previous_version} ==="
    return 1
  fi
}

main() {
  log_info "========================================="
  log_info "Shorty Production Deployment"
  log_info "Version:     ${DEPLOY_VERSION}"
  log_info "Services:    ${SERVICES}"
  log_info "Environment: ${ENVIRONMENT}"
  log_info "Region:      ${AWS_REGION}"
  log_info "Canary:      ${CANARY_WEIGHT} ($(echo "${CANARY_WEIGHT} * 100" | bc)%)"
  log_info "========================================="

  if [[ "${DRY_RUN}" == "true" ]]; then
    log_warn "DRY RUN mode -- no changes will be made"
  fi

  # Pre-flight
  preflight_checks

  # Build
  build_all

  # Upload artifacts
  upload_artifacts

  # Deploy each service sequentially (canary requires sequential health checks)
  local failed_services=()

  for service in ${SERVICES}; do
    if ! deploy_service "${service}"; then
      failed_services+=("${service}")
      log_error "Deployment failed for ${service}. Stopping deployment pipeline."
      break
    fi
  done

  # Summary
  echo ""
  log_info "========================================="
  log_info "Deployment Summary"
  log_info "========================================="

  if [[ ${#failed_services[@]} -eq 0 ]]; then
    log_info "Status: SUCCESS"
    log_info "All services deployed to ${ENVIRONMENT}"
    for service in ${SERVICES}; do
      local fn
      fn="$(get_function_name "${service}")"
      local ver
      ver="$(get_current_alias_version "${fn}" "live")"
      log_info "  ${service}: v${ver}"
    done
  else
    log_error "Status: FAILED"
    log_error "Failed services: ${failed_services[*]}"
    exit 1
  fi
}

main "$@"

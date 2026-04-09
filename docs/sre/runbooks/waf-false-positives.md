# Runbook: WAF False Positives

**Alert:** Manual detection (user reports of blocked requests) / elevated WAF `BlockedRequests` metric
**Severity:** SEV3 (some legitimate users blocked) / SEV2 (widespread blocking affecting availability)
**SLO Impact:** WAF false positives cause legitimate redirect requests to receive 403 Forbidden instead of a redirect. These count against the availability SLO (99.9% target) since they are not successful responses.

---

## Symptoms

- Users reporting 403 Forbidden errors when accessing short links.
- Elevated `BlockedRequests` metric on the WAF Web ACL.
- Customer support tickets about "broken links" that work from some networks but not others.
- WAF `AllowedRequests` dropping while traffic is stable or growing.
- Specific geographic regions or IP ranges unable to access the service.

---

## Diagnosis

### 1. Check WAF metrics

```bash
# Get Web ACL ID
WAF_ACL=$(aws wafv2 list-web-acls --scope REGIONAL \
  --query "WebACLs[?Name=='shorty-waf'].Id" --output text)

# Blocked requests over last 30 min
aws cloudwatch get-metric-statistics \
  --namespace AWS/WAFV2 \
  --metric-name BlockedRequests \
  --dimensions Name=WebACL,Value=shorty-waf Name=Region,Value={region} Name=Rule,Value=ALL \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum

# Blocked requests per rule
for rule in rate-limit-rule ip-reputation-rule geo-block-rule sql-injection-rule; do
  echo "=== $rule ==="
  aws cloudwatch get-metric-statistics \
    --namespace AWS/WAFV2 \
    --metric-name BlockedRequests \
    --dimensions Name=WebACL,Value=shorty-waf Name=Region,Value={region} Name=Rule,Value=$rule \
    --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
    --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
    --period 60 --statistics Sum
done
```

### 2. Analyze WAF sampled requests

```bash
# Get sampled requests (shows actual blocked requests with details)
aws wafv2 get-sampled-requests \
  --web-acl-arn arn:aws:wafv2:{region}:{account}:regional/webacl/shorty-waf/$WAF_ACL \
  --rule-metric-name ALL \
  --scope REGIONAL \
  --time-window "StartTime=$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S),EndTime=$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --max-items 100
```

Examine the output for:
- **Source IPs:** Are legitimate user IPs being blocked?
- **URI paths:** Are normal `/{code}` paths being blocked?
- **Rule matched:** Which specific rule is triggering?
- **Country:** Is a geo-restriction blocking legitimate users?
- **User-Agent:** Are common browsers being blocked?

### 3. Check WAF logging (if enabled)

```bash
# WAF logs in S3 or CloudWatch Logs
# If logging to CloudWatch Logs:
aws logs filter-log-events \
  --log-group-name aws-waf-logs-shorty \
  --filter-pattern '{ $.action = "BLOCK" }' \
  --start-time "$(date -u -d '30 minutes ago' +%s)000" \
  --limit 50
```

### 4. Check rate-based rule thresholds

```bash
# Get current rate-based rule configuration
aws wafv2 get-web-acl \
  --name shorty-waf --scope REGIONAL --id $WAF_ACL \
  --query 'WebACL.Rules[?Statement.RateBasedStatement].{Name: Name, Limit: Statement.RateBasedStatement.Limit, AggregateKeyType: Statement.RateBasedStatement.AggregateKeyType}'

# Check IPs currently rate-limited
aws wafv2 get-rate-based-statement-managed-keys \
  --web-acl-name shorty-waf \
  --web-acl-id $WAF_ACL \
  --scope REGIONAL \
  --rule-name rate-limit-rule
```

### 5. Test from the affected user's perspective

```bash
# Test a known short code with curl (check for 403)
curl -v "https://{cloudfront_domain}/{code}" \
  -H "X-Forwarded-For: {reported_user_ip}"

# Test with the specific User-Agent reported
curl -v "https://{cloudfront_domain}/{code}" \
  -H "User-Agent: {reported_user_agent}"
```

---

## Mitigation

### Step 1: Identify the offending rule

From the sampled requests analysis, identify which rule is causing false positives.

### Step 2: Switch the rule to Count mode (stop blocking immediately)

```bash
# Get current Web ACL config
aws wafv2 get-web-acl --name shorty-waf --scope REGIONAL --id $WAF_ACL > /tmp/waf-config.json

# Edit the rule action from BLOCK to COUNT
# (use jq or manual edit to change the specific rule)
# Then update the Web ACL with the modified config

aws wafv2 update-web-acl \
  --name shorty-waf \
  --scope REGIONAL \
  --id $WAF_ACL \
  --lock-token "$(jq -r '.LockToken' /tmp/waf-config.json)" \
  --default-action '{"Allow": {}}' \
  --rules file:///tmp/updated-rules.json \
  --visibility-config file:///tmp/visibility-config.json
```

Note: Switching to Count mode means the rule still logs matches but does not block. This is the safest way to stop false positives immediately while preserving visibility.

### Step 3: Add specific IP to allowlist (targeted fix)

If a known-good IP or range is being blocked:

```bash
# Create or update an IP set for the allowlist
aws wafv2 create-ip-set \
  --name shorty-allowlist \
  --scope REGIONAL \
  --ip-address-version IPV4 \
  --addresses "203.0.113.0/24"

# Or update existing allowlist
aws wafv2 update-ip-set \
  --name shorty-allowlist \
  --scope REGIONAL \
  --id {ip_set_id} \
  --lock-token {lock_token} \
  --addresses "203.0.113.0/24" "198.51.100.0/24"
```

Then ensure the allowlist rule has higher priority (lower numeric priority) than the blocking rule.

### Step 4: Adjust rate-based rule threshold

If the rate-based rule threshold is too aggressive:

```bash
# Increase the rate limit (requests per 5-minute window per IP)
# Current: check the get-web-acl output
# Adjust to a higher value (e.g., from 2000 to 5000)
```

Update the rate-based rule in the Web ACL configuration with a higher limit.

### Step 5: Adjust managed rule group exclusions

If an AWS Managed Rule Group (e.g., `AWSManagedRulesCommonRuleSet`) is causing false positives:

```bash
# Identify the specific rule within the group that is matching
# From sampled requests, note the RuleWithinRuleGroup value

# Add a rule exclusion (excluded rule still evaluates but action is overridden to Count)
# This is done in the managed rule group statement's ExcludedRules array
```

---

## Prevention

1. **Count mode first:** When adding new WAF rules, always deploy in Count mode for at least 48 hours. Monitor sampled requests before switching to Block.
2. **Rate limit tuning:** Set rate-based rule thresholds based on observed traffic patterns, not arbitrary values. Review P95 per-IP request rate monthly.
3. **Allowlist for partners:** If known partners or monitoring services need higher rate limits, add their IP ranges to the allowlist proactively.
4. **WAF logging:** Keep WAF logging enabled to S3 or CloudWatch Logs. This is essential for post-incident analysis of false positives.
5. **Regular rule review:** Review WAF rules quarterly. Remove rules that have zero matches over 90 days. Update managed rule group versions.
6. **Synthetic monitoring:** Include WAF-protected paths in synthetic monitoring canaries. If the canary gets blocked, alert immediately.
7. **IP hashing compliance:** Remember that Shorty never stores IP addresses in plain text (SHA-256 with salt). WAF logs contain raw IPs, so restrict access to WAF log data.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Widespread blocking affecting > 1% of users | Declare SEV2; switch offending rule to Count mode immediately |
| Managed rule group false positive | Open AWS Support case for WAF rule tuning guidance |
| Rate-based rule blocking legitimate high-traffic partner | Engineering Lead -- add partner to allowlist, review rate limit policy |
| Geographic block affecting legitimate users | Engineering Lead -- review geo-restriction policy |
| WAF configuration change unauthorized | Security team -- review CloudTrail for unauthorized API calls |
| Cannot identify which rule is blocking | Enable WAF logging if not already enabled; escalate to SRE team |

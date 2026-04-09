# WAF Configuration

> AWS Specialist specification for DevOps implementation.
> Reference: ADR-009, requirements-init.md Sections 7.1, 7.2, 7.3, 13.

---

## 1. Web ACL Overview

| Setting | Value |
|---|---|
| Name | `shorty-waf` |
| Scope | `CLOUDFRONT` (must be created in us-east-1) |
| Default action | Allow |
| Associated resource | CloudFront distribution |
| Logging | CloudWatch Logs (`aws-waf-logs-shorty`) |

---

## 2. Rule Ordering (priority ascending -- lower number evaluated first)

| Priority | Rule Name | Type | Action | Scope | Rationale |
|---|---|---|---|---|---|
| 1 | `shorty-ip-blocklist` | Custom IP set | BLOCK | All paths | Manual blocklist for known bad actors; checked first for fast rejection |
| 2 | `AWS-AWSManagedRulesCommonRuleSet` | AWS Managed | BLOCK | All paths | OWASP Top 10 baseline (SQLi, XSS, path traversal, etc.) |
| 3 | `AWS-AWSManagedRulesKnownBadInputsRuleSet` | AWS Managed | BLOCK | All paths | Log4j, SSRF, bad inputs |
| 4 | `AWS-AWSManagedRulesBotControlRuleSet` | AWS Managed | COUNT (then BLOCK) | All paths | Bot detection -- start in COUNT mode for 1 week to baseline |
| 5 | `AWS-AWSManagedRulesAmazonIpReputationList` | AWS Managed | BLOCK | All paths | Block IPs from known bad reputation lists |
| 6 | `shorty-rate-limit-global` | Rate-based | BLOCK | All paths | 1,000 req/5min per IP -- broad DDoS mitigation |
| 7 | `shorty-rate-limit-password` | Rate-based | BLOCK | `/p/*` | 10 req/5min per IP -- password brute-force protection |
| 8 | `shorty-rate-limit-create` | Rate-based | BLOCK | `/api/v1/shorten` | 20 creates/5min per IP -- abuse-specific limit |
| 9 | `shorty-captcha-create` | Custom (byte match) | CAPTCHA | `/api/v1/shorten` | Bot-submitted link creation challenge |
| 10 | `shorty-block-suspicious-ua` | Custom (regex) | BLOCK | All paths | Block obvious bot User-Agents |
| 11 | `shorty-geo-block` | Custom (geo match) | BLOCK | All paths | Optional geo-restriction (disabled by default) |

---

## 3. Rule Details

### 3.1 IP Blocklist (Priority 1)

Manually maintained IP set for confirmed bad actors. Updated via automation or incident response.

```
Type:   IPSetReferenceStatement
IP Set: shorty-blocked-ips (IPv4) + shorty-blocked-ips-v6 (IPv6)
Action: BLOCK
```

### 3.2 AWS Managed Rules: Common Rule Set (Priority 2)

Covers OWASP Top 10 including SQL injection, XSS, local file inclusion, and more.

```
Vendor:          AWS
Rule group:      AWSManagedRulesCommonRuleSet
Override action: None (use rule group defaults)
Excluded rules:  (add after baseline analysis if false positives occur)
  - SizeRestrictions_BODY (if needed for longer URL submissions)
  - CrossSiteScripting_BODY (evaluate after launch)
```

### 3.3 AWS Managed Rules: Known Bad Inputs (Priority 3)

Protects against Log4j/JNDI, SSRF patterns, and other known exploit payloads.

```
Vendor:          AWS
Rule group:      AWSManagedRulesKnownBadInputsRuleSet
Override action: None
Excluded rules:  None
```

### 3.4 AWS Managed Rules: Bot Control (Priority 4)

**IMPORTANT:** Deploy in COUNT mode for the first 7 days. Analyze `aws-waf-logs-shorty` before promoting to BLOCK. Bot Control may flag legitimate crawlers (Googlebot, Bingbot) that should be allowed.

```
Vendor:          AWS
Rule group:      AWSManagedRulesBotControlRuleSet
Level:           COMMON ($10/month, not TARGETED)
Override action: COUNT (initial), then selectively BLOCK/CAPTCHA
```

Bot classification behavior:

| Bot Category | Action |
|---|---|
| Verified bots (Googlebot, Bingbot) | ALLOW (good for SEO on redirect pages) |
| Unverified bots | CAPTCHA on `/api/v1/shorten`; rate-limited on redirect |
| Targeted bots (scrapers, crawlers) | BLOCK after baseline period |

### 3.5 IP Reputation List (Priority 5)

Blocks IPs from AWS threat intelligence feeds (botnets, known attackers).

```
Vendor:          AWS
Rule group:      AWSManagedRulesAmazonIpReputationList
Override action: None
```

### 3.6 Rate-Based Rule: Global (Priority 6)

```
Limit:              1,000 requests per 5-minute window
Aggregate key:      IP
Action:             BLOCK
Scope:              All paths
Evaluation window:  300 seconds
```

Note: WAF rate-based rules have a minimum threshold of 100 requests per 5 minutes. This is coarser than the application-level Redis rate limiter (200 req/min). Both layers work together: WAF catches bulk floods, Redis handles fine-grained per-endpoint limits.

### 3.7 Rate-Based Rule: Password Endpoint (Priority 7)

```
Limit:              10 requests per 5-minute window
Aggregate key:      IP
Action:             BLOCK
Scope:              URI path starts with /p/
Evaluation window:  300 seconds
```

Protects against credential stuffing on password-protected short links.

### 3.8 Rate-Based Rule: Link Creation (Priority 8)

```
Limit:              20 requests per 5-minute window
Aggregate key:      IP
Action:             BLOCK
Scope:              URI path = /api/v1/shorten, HTTP method = POST
Evaluation window:  300 seconds
```

### 3.9 CAPTCHA on Link Creation (Priority 9)

Triggers CAPTCHA challenge for suspicious traffic on the link creation endpoint.

```
Statement:        ByteMatchStatement on URI path = /api/v1/shorten
                  AND HTTP method = POST
                  AND NOT header X-Captcha-Verified = true
Action:           CAPTCHA
Immunity time:    300 seconds (5 minutes after solving)
```

### 3.10 Block Suspicious User-Agents (Priority 10)

```
Statement:    RegexPatternSetReferenceStatement
Patterns:     
  - ^$                           (empty User-Agent)
  - (?i)(curl|wget|python|scrapy|httpclient|java/)
  - (?i)(nikto|sqlmap|nmap|masscan|zgrab)
Field:        SingleHeader (User-Agent)
Action:       BLOCK
```

Note: Legitimate API clients using `curl` may be blocked. If programmatic API access is needed (post-MVP developer API), add an exception for requests with valid API keys.

### 3.11 Geo-Blocking (Priority 11)

Disabled by default. Enable selectively for:

- OFAC-sanctioned countries (compliance)
- Countries generating disproportionate attack traffic (incident response)

```
Statement:    GeoMatchStatement
Countries:    [] (empty -- configure per requirement)
Action:       BLOCK
```

---

## 4. Rule Actions Reference

| Action | Behavior | Use Case |
|---|---|---|
| BLOCK | Request denied with 403 | Known bad traffic, rate limit violations |
| COUNT | Request allowed, logged for analysis | Baseline period for new rules |
| CAPTCHA | Client must solve CAPTCHA, immunity for 300s | Suspicious automated traffic |
| CHALLENGE | Silent JavaScript challenge, immunity for 300s | Less intrusive than CAPTCHA |
| ALLOW | Explicit allow, skips remaining rules | Whitelisted IPs (use sparingly) |

---

## 5. WAF Logging

### CloudWatch Logs

```
Log group:        aws-waf-logs-shorty
Retention:        90 days
Filter:           Log all BLOCK and CAPTCHA actions
                  Sample 10% of ALLOW actions (cost control)
```

### Logging Configuration

```
Redacted fields:
  - Authorization header (contains JWT)
  - Cookie header (contains session tokens)

Full logging for:
  - /api/v1/shorten  (100% sample rate)
  - /p/*             (100% sample rate)

Default sample rate: 10% for all other paths
```

---

## 6. WAF Metrics and Alarms

### CloudWatch Metrics (auto-generated by WAF)

| Metric | Description |
|---|---|
| `shorty-waf-AllowedRequests` | Total allowed requests |
| `shorty-waf-BlockedRequests` | Total blocked requests |
| `shorty-waf-CountedRequests` | Total counted requests |
| `shorty-rate-limit-BlockedRequests` | Requests blocked by rate limiting |
| `shorty-bot-control-CountedRequests` | Bot Control detections |

### CloudWatch Alarms

| Alarm | Threshold | Action |
|---|---|---|
| High block rate | > 1000 blocked requests/min for 5 min | SNS notification to ops team |
| Rate limit spike | > 100 rate-limited IPs/5min | SNS notification |
| Bot control spike | > 500 bot detections/min for 5 min | SNS notification |
| WAF error rate | > 10 WAF errors/min | SNS notification (WAF misconfiguration) |

---

## 7. Terraform Example

```hcl
# --- IP Set for manual blocklist ---
resource "aws_wafv2_ip_set" "blocked_ips" {
  provider           = aws.us_east_1
  name               = "shorty-blocked-ips"
  scope              = "CLOUDFRONT"
  ip_address_version = "IPV4"
  addresses          = []  # Populated via automation or console

  tags = {
    Project = "shorty"
  }
}

# --- Regex Pattern Set for suspicious User-Agents ---
resource "aws_wafv2_regex_pattern_set" "suspicious_ua" {
  provider = aws.us_east_1
  name     = "shorty-suspicious-ua"
  scope    = "CLOUDFRONT"

  regular_expression {
    regex_string = "(?i)(nikto|sqlmap|nmap|masscan|zgrab)"
  }
  regular_expression {
    regex_string = "(?i)(scrapy|httpclient|python-requests)"
  }
  regular_expression {
    regex_string = "^$"
  }

  tags = {
    Project = "shorty"
  }
}

# --- Web ACL ---
resource "aws_wafv2_web_acl" "shorty" {
  provider = aws.us_east_1
  name     = "shorty-waf"
  scope    = "CLOUDFRONT"

  default_action {
    allow {}
  }

  # --- Priority 1: IP Blocklist ---
  rule {
    name     = "shorty-ip-blocklist"
    priority = 1

    action {
      block {}
    }

    statement {
      ip_set_reference_statement {
        arn = aws_wafv2_ip_set.blocked_ips.arn
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-ip-blocklist"
    }
  }

  # --- Priority 2: AWS Managed Rules - Common Rule Set ---
  rule {
    name     = "aws-common-rules"
    priority = 2

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-common-rules"
    }
  }

  # --- Priority 3: AWS Managed Rules - Known Bad Inputs ---
  rule {
    name     = "aws-known-bad-inputs"
    priority = 3

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-known-bad-inputs"
    }
  }

  # --- Priority 4: AWS Managed Rules - Bot Control (COUNT mode initially) ---
  rule {
    name     = "aws-bot-control"
    priority = 4

    override_action {
      count {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesBotControlRuleSet"
        vendor_name = "AWS"

        managed_rule_group_configs {
          aws_managed_rules_bot_control_rule_set {
            inspection_level = "COMMON"
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-bot-control"
    }
  }

  # --- Priority 5: AWS Managed Rules - IP Reputation List ---
  rule {
    name     = "aws-ip-reputation"
    priority = 5

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-ip-reputation"
    }
  }

  # --- Priority 6: Rate-based rule - Global (1000 req/5min per IP) ---
  rule {
    name     = "shorty-rate-limit-global"
    priority = 6

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 1000
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-rate-limit-global"
    }
  }

  # --- Priority 7: Rate-based rule - Password endpoint (10 req/5min) ---
  rule {
    name     = "shorty-rate-limit-password"
    priority = 7

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 100  # WAF minimum is 100; effective with scope_down
        aggregate_key_type = "IP"

        scope_down_statement {
          byte_match_statement {
            search_string         = "/p/"
            positional_constraint = "STARTS_WITH"
            field_to_match {
              uri_path {}
            }
            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-rate-limit-password"
    }
  }

  # --- Priority 8: Rate-based rule - Link creation (20 req/5min) ---
  rule {
    name     = "shorty-rate-limit-create"
    priority = 8

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 100  # WAF minimum is 100
        aggregate_key_type = "IP"

        scope_down_statement {
          and_statement {
            statement {
              byte_match_statement {
                search_string         = "/api/v1/shorten"
                positional_constraint = "EXACTLY"
                field_to_match {
                  uri_path {}
                }
                text_transformation {
                  priority = 0
                  type     = "LOWERCASE"
                }
              }
            }
            statement {
              byte_match_statement {
                search_string         = "POST"
                positional_constraint = "EXACTLY"
                field_to_match {
                  method {}
                }
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
          }
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-rate-limit-create"
    }
  }

  # --- Priority 9: CAPTCHA on link creation ---
  rule {
    name     = "shorty-captcha-create"
    priority = 9

    action {
      captcha {}
    }

    statement {
      and_statement {
        statement {
          byte_match_statement {
            search_string         = "/api/v1/shorten"
            positional_constraint = "EXACTLY"
            field_to_match {
              uri_path {}
            }
            text_transformation {
              priority = 0
              type     = "LOWERCASE"
            }
          }
        }
        statement {
          byte_match_statement {
            search_string         = "POST"
            positional_constraint = "EXACTLY"
            field_to_match {
              method {}
            }
            text_transformation {
              priority = 0
              type     = "NONE"
            }
          }
        }
      }
    }

    captcha_config {
      immunity_time_property {
        immunity_time = 300
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-captcha-create"
    }
  }

  # --- Priority 10: Block suspicious User-Agents ---
  rule {
    name     = "shorty-block-suspicious-ua"
    priority = 10

    action {
      block {}
    }

    statement {
      regex_pattern_set_reference_statement {
        arn = aws_wafv2_regex_pattern_set.suspicious_ua.arn
        field_to_match {
          single_header {
            name = "user-agent"
          }
        }
        text_transformation {
          priority = 0
          type     = "NONE"
        }
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "shorty-suspicious-ua"
    }
  }

  # --- Priority 11: Geo-blocking (disabled by default) ---
  # Uncomment and configure when needed:
  #
  # rule {
  #   name     = "shorty-geo-block"
  #   priority = 11
  #
  #   action {
  #     block {}
  #   }
  #
  #   statement {
  #     geo_match_statement {
  #       country_codes = ["XX", "YY"]  # Replace with actual country codes
  #     }
  #   }
  #
  #   visibility_config {
  #     sampled_requests_enabled   = true
  #     cloudwatch_metrics_enabled = true
  #     metric_name                = "shorty-geo-block"
  #   }
  # }

  visibility_config {
    sampled_requests_enabled   = true
    cloudwatch_metrics_enabled = true
    metric_name                = "shorty-waf"
  }

  tags = {
    Project     = "shorty"
    Environment = var.environment
  }
}

# --- WAF Logging ---
resource "aws_wafv2_web_acl_logging_configuration" "shorty" {
  provider                = aws.us_east_1
  log_destination_configs = [aws_cloudwatch_log_group.waf_logs.arn]
  resource_arn            = aws_wafv2_web_acl.shorty.arn

  logging_filter {
    default_behavior = "DROP"

    filter {
      behavior    = "KEEP"
      requirement = "MEETS_ANY"

      condition {
        action_condition {
          action = "BLOCK"
        }
      }
      condition {
        action_condition {
          action = "CAPTCHA"
        }
      }
      condition {
        action_condition {
          action = "COUNT"
        }
      }
    }
  }

  redacted_fields {
    single_header {
      name = "authorization"
    }
  }

  redacted_fields {
    single_header {
      name = "cookie"
    }
  }
}

resource "aws_cloudwatch_log_group" "waf_logs" {
  provider          = aws.us_east_1
  name              = "aws-waf-logs-shorty"
  retention_in_days = 90

  tags = {
    Project = "shorty"
  }
}

# --- CloudWatch Alarms ---
resource "aws_cloudwatch_metric_alarm" "waf_high_block_rate" {
  provider            = aws.us_east_1
  alarm_name          = "shorty-waf-high-block-rate"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "BlockedRequests"
  namespace           = "AWS/WAFV2"
  period              = 300
  statistic           = "Sum"
  threshold           = 5000
  alarm_description   = "WAF blocking > 5000 requests in 5 minutes"
  alarm_actions       = [aws_sns_topic.ops_alerts.arn]

  dimensions = {
    WebACL = aws_wafv2_web_acl.shorty.name
    Region = "us-east-1"
    Rule   = "ALL"
  }
}
```

---

## 8. Operational Runbook

### Deploying new WAF rules

1. Always deploy new rules in COUNT mode first.
2. Monitor `aws-waf-logs-shorty` for 7 days.
3. Analyze false positive rate.
4. Promote to BLOCK only after confirming acceptable false positive rate (<0.1%).

### Responding to DDoS

1. Check WAF metrics for blocked request volume.
2. If attack traffic exceeds WAF capacity, add attacking IP ranges to `shorty-blocked-ips`.
3. Enable geo-blocking for source countries if attack is geographically concentrated.
4. Consider enabling AWS Shield Advanced if sustained attack (requires business decision due to $3,000/month cost).

### Handling false positives

1. Identify the blocking rule from WAF logs (`terminatingRuleId` field).
2. Add specific rule exclusion to the managed rule group (e.g., exclude `CrossSiteScripting_BODY` from CRS).
3. Re-deploy and monitor.

### WAF minimum rate limit caveat

AWS WAF rate-based rules have a minimum threshold of 100 requests per 5 minutes. For finer-grained rate limiting (e.g., 5 links/hour per IP), rely on the application-level Redis sliding-window rate limiter (see ADR-006). The two layers are complementary:

- WAF: coarse, edge-level, blocks before Lambda invocation (saves cost)
- Redis: fine-grained, app-level, per-endpoint and per-user limits

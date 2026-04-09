# GDPR & Privacy Compliance -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T05)
**References:** GDPR (EU 2016/679), security-architecture.md, threat-model.md

---

## 1. Data Inventory

### 1.1 Personal Data Map

| Data Element | Classification | Storage Location | Retention | Legal Basis | Encrypted at Rest | Encrypted in Transit |
|---|---|---|---|---|---|---|
| Email address | PII | Cognito User Pool | Until account deletion | Contract (Art. 6(1)(b)) | Yes (Cognito-managed) | Yes (TLS) |
| Email address (copy) | PII | DynamoDB `users` table | Until account deletion | Contract | Yes (KMS CMK) | Yes (TLS) |
| IP address (hashed) | Pseudonymous PII | DynamoDB `clicks` table | 90 days (TTL auto-delete) | Legitimate interest (Art. 6(1)(f)) | Yes (KMS CMK) | Yes (TLS) |
| Raw IP address | PII | CloudFront access logs (S3) | 14 days | Legitimate interest | Yes (S3 SSE) | Yes (TLS) |
| Raw IP address | PII | CloudWatch Logs | 14 days | Legitimate interest | Yes (KMS CMK) | Yes (TLS) |
| User-Agent (hashed) | Pseudonymous | DynamoDB `clicks` table | 90 days (TTL) | Legitimate interest | Yes (KMS CMK) | Yes (TLS) |
| Referer domain | Behavioral | DynamoDB `clicks` table | 90 days (TTL) | Legitimate interest | Yes (KMS CMK) | Yes (TLS) |
| Country (GeoIP) | Behavioral | DynamoDB `clicks` table | 90 days (TTL) | Legitimate interest | Yes (KMS CMK) | Yes (TLS) |
| Original URLs | Potentially sensitive | DynamoDB `links` table | Until link deletion | Contract | Yes (KMS CMK) | Yes (TLS) |
| Click patterns | Behavioral | DynamoDB `clicks` table | 90 days (TTL) | Contract | Yes (KMS CMK) | Yes (TLS) |
| Link titles | Not PII (usually) | DynamoDB `links` table | Until link deletion | Contract | Yes (KMS CMK) | Yes (TLS) |
| Password hash (bcrypt) | Security credential | DynamoDB `links` table | Until link deletion | Contract | Yes (KMS CMK) | Yes (TLS) |
| Cognito sub (user ID) | Pseudonymous identifier | DynamoDB `links`/`users` | Until account deletion | Contract | Yes (KMS CMK) | Yes (TLS) |
| JWT access token | Authentication | httpOnly cookie (client) | 1 hour (token expiry) | Contract | N/A (client-side) | Yes (TLS, Secure flag) |
| JWT refresh token | Authentication | httpOnly cookie (client) | 7 days (token expiry) | Contract | N/A (client-side) | Yes (TLS, Secure flag) |

### 1.2 Data Flow Diagram

```
User (browser)
  |
  | [Email, password, OAuth token] -- TLS 1.2+
  v
CloudFront -- [Raw IP in access logs, 14-day retention]
  |
  v
API Gateway
  |
  v
Lambda (redirect)
  |-- SHA-256(IP + salt) --> ip_hash (irreversible)
  |-- SHA-256(User-Agent) --> user_agent_hash (irreversible)
  |-- GeoIP(IP) --> country code (aggregation only)
  |-- Raw IP discarded after hashing (never stored in application layer)
  |
  +---> SQS (ip_hash, user_agent_hash, country, referer_domain)
  |       |
  |       v
  |     Worker Lambda --> DynamoDB clicks (90-day TTL)
  |
  +---> Redis (cache: no PII, only link metadata + has_password flag)

Lambda (api)
  |-- Cognito JWT validation (sub claim = user identity)
  |-- owner_id stored on links (Cognito sub, not email)
  |-- Email stored only in users table (for profile display)
```

---

## 2. Privacy by Design Principles

### 2.1 Data Minimization

- **IP addresses** are hashed immediately upon receipt using SHA-256 with a secret salt. The raw IP is never written to DynamoDB or Redis. It exists only in Lambda process memory during the hash computation.
- **User-Agent** strings are hashed (SHA-256) before storage. Only the hash is stored; the original string is discarded.
- **Referer** is stored as the domain portion only (`referer_domain`), not the full URL (which could contain query parameters with PII).
- **Email** is stored only in the `users` table and Cognito. It is never denormalized into `links` or `clicks` tables.
- **Country** is derived from GeoIP lookup and stored as a 2-character ISO code. The IP-to-country mapping is one-way and coarse-grained.

### 2.2 Purpose Limitation

| Data | Purpose | Not Used For |
|---|---|---|
| IP hash | Rate limiting, unique click counting | User tracking across links, advertising |
| User-Agent hash | Device type classification (desktop/mobile/bot) | Browser fingerprinting |
| Referer domain | Traffic source analytics | Cross-site tracking |
| Country | Geographic distribution analytics | Targeted content, pricing discrimination |
| Email | Account identification, notifications | Marketing (unless explicit consent) |

### 2.3 Storage Limitation

| Data | Retention | Deletion Mechanism |
|---|---|---|
| Click events | 90 days | DynamoDB TTL (automatic, serverless) |
| CloudFront logs | 14 days | S3 lifecycle policy (automatic) |
| CloudWatch Logs | 14 days | Log group retention setting (automatic) |
| User profile | Until account deletion | User-initiated via `DELETE /api/v1/me` |
| Links | Until link deletion or account deletion | User-initiated or cascading delete |

### 2.4 IP Salt Rotation as Privacy Enhancement

The IP hash salt rotates every 90 days (see `secrets-management.md`). After rotation:
- Old `ip_hash` values in the `clicks` table cannot be linked to new `ip_hash` values
- This provides natural unlinkability across rotation boundaries
- Combined with the 90-day click TTL, old click data is both unlinkable and auto-deleted

---

## 3. Data Subject Rights Implementation

### 3.1 Right to Access (GDPR Art. 15)

**Endpoint:** `GET /api/v1/me/export`

Returns all personal data associated with the authenticated user in JSON format.

```bash
curl -X GET https://api.shorty.io/api/v1/me/export \
  -H "Cookie: access_token=<jwt>"
```

**Response (200 OK):**

```json
{
  "export_date": "2026-04-05T12:00:00Z",
  "user": {
    "id": "USER#abc123",
    "email": "user@example.com",
    "plan": "free",
    "created_at": "2025-01-15T08:00:00Z"
  },
  "links": [
    {
      "code": "abc1234",
      "original_url": "https://example.com/long-url",
      "title": "My Link",
      "has_password": false,
      "click_count": 42,
      "created_at": "2025-06-01T10:00:00Z",
      "expires_at": null,
      "is_active": true
    }
  ],
  "click_summary": {
    "total_clicks": 42,
    "note": "Individual click events contain only pseudonymized data (IP hashes, User-Agent hashes) and are not included in this export as they are not personally identifiable."
  }
}
```

**Rate limit:** 1 export per 30 days per user (prevents abuse).

**Implementation notes:**
- Query `users` table for profile
- Query `links` table via `owner_id-created_at-index` GSI for all user links
- Click events are pseudonymized and not included (ip_hash is not reversible to a person)
- Response is generated synchronously for users with < 1,000 links
- For users with > 1,000 links, return 202 Accepted and deliver via email

### 3.2 Right to Erasure (GDPR Art. 17)

**Endpoint:** `DELETE /api/v1/me`

Deletes all user data: profile, all links, and all associated click records.

```bash
curl -X DELETE https://api.shorty.io/api/v1/me \
  -H "Cookie: access_token=<jwt>" \
  -H "X-Confirm-Delete: true"
```

**Response (202 Accepted):**

```json
{
  "status": "deletion_initiated",
  "message": "Your account and all associated data will be deleted within 24 hours.",
  "confirmation_email": "user@example.com"
}
```

**Implementation:**

1. Mark user as `deletion_pending` in `users` table (prevents new logins)
2. Revoke all Cognito sessions: `AdminUserGlobalSignOut`
3. Enqueue deletion job to SQS:
   ```json
   {
     "action": "delete_user",
     "user_id": "USER#abc123",
     "requested_at": "2026-04-05T12:00:00Z"
   }
   ```
4. Worker processes deletion:
   - Query all links by `owner_id` via GSI
   - For each link: delete all click records (`BatchWriteItem` delete requests)
   - Delete all link records
   - Delete user record from `users` table
   - Delete Cognito user: `AdminDeleteUser`
5. Send confirmation email after completion

**Data that remains after deletion:**
- CloudFront access logs (raw IP) for up to 14 days -- cannot be selectively deleted from S3 log files
- CloudWatch Logs entries for up to 14 days -- log entries are immutable
- These are retained under the legitimate interest basis for security incident investigation (GDPR Art. 17(3)(e))

### 3.3 Right to Data Portability (GDPR Art. 20)

**Endpoint:** Same as Right to Access -- `GET /api/v1/me/export`

The export format is machine-readable JSON. Users can download and transfer their link data to another service.

### 3.4 Right to Rectification (GDPR Art. 16)

**Endpoint:** `PATCH /api/v1/me`

Users can update their email address and profile information. Email changes require re-verification via Cognito.

```bash
curl -X PATCH https://api.shorty.io/api/v1/me \
  -H "Cookie: access_token=<jwt>" \
  -H "Content-Type: application/json" \
  -d '{"email": "new@example.com"}'
```

### 3.5 Right to Restriction of Processing (GDPR Art. 18)

Users can deactivate individual links without deleting them. Deactivated links stop collecting click data.

```bash
curl -X PATCH https://api.shorty.io/api/v1/links/abc1234 \
  -H "Cookie: access_token=<jwt>" \
  -H "Content-Type: application/json" \
  -d '{"is_active": false}'
```

---

## 4. Data Processing Agreement (DPA) Considerations

### 4.1 AWS as Data Processor

AWS acts as a data processor under GDPR. Key considerations:

| Area | AWS Coverage |
|---|---|
| DPA | AWS Customer Agreement includes GDPR DPA (AWS Data Processing Addendum) |
| Sub-processors | AWS publishes a list of sub-processors; notifications via SNS |
| Data location | All data stored in `us-east-1` (or configured region). For EU users, consider `eu-west-1` |
| Compliance certifications | SOC 2, ISO 27001, ISO 27018 (cloud privacy) |
| Data deletion | AWS deletes data when resources are terminated; KMS key deletion has 7-30 day waiting period |

### 4.2 Google as Data Processor (OAuth + Safe Browsing)

- **Google OAuth:** User consents to Google sharing email/profile with Shorty during OAuth flow. Only `email` and `profile` scopes are requested.
- **Google Safe Browsing:** URLs submitted to the API for malware/phishing checks. URLs are not PII themselves, but may contain PII in query parameters. Consider hashing URL prefixes (Safe Browsing v4 Update API supports this).

### 4.3 Region Selection for GDPR Compliance

For EU-focused deployments:
- Primary region: `eu-west-1` (Ireland)
- DynamoDB Global Tables replica: `eu-central-1` (Frankfurt)
- CloudFront: edge locations are global (acceptable under SCCs for CDN purposes)
- Cognito: deployed in the same region as the application

---

## 5. Cookie Policy

### 5.1 Cookies Used

| Cookie Name | Type | Purpose | Duration | httpOnly | Secure | SameSite |
|---|---|---|---|---|---|---|
| `access_token` | Functional (strictly necessary) | JWT authentication | 1 hour (session) | Yes | Yes | Strict |
| `refresh_token` | Functional (strictly necessary) | JWT refresh | 7 days | Yes | Yes | Strict |
| `csrf_session` | Functional (strictly necessary) | CSRF protection for password forms | Session | Yes | Yes | Strict |

### 5.2 Cookie Consent

**No cookie consent banner is required.** All cookies are strictly necessary for the service to function (authentication, CSRF protection). Per GDPR Recital 30 and the ePrivacy Directive (Art. 5(3)), strictly necessary cookies are exempt from consent requirements.

**Shorty does not use:**
- Analytics cookies (no Google Analytics, no tracking pixels)
- Advertising cookies
- Third-party cookies
- Local storage for tracking purposes

### 5.3 Cookie Security Configuration

```go
func SetAuthCookie(w http.ResponseWriter, name, value string, maxAge int) {
    http.SetCookie(w, &http.Cookie{
        Name:     name,
        Value:    value,
        Path:     "/",
        Domain:   "", // defaults to the request host (no subdomain sharing)
        MaxAge:   maxAge,
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteStrictMode,
    })
}

func ClearAuthCookie(w http.ResponseWriter, name string) {
    http.SetCookie(w, &http.Cookie{
        Name:     name,
        Value:    "",
        Path:     "/",
        MaxAge:   -1,
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteStrictMode,
    })
}
```

---

## 6. Privacy Policy Requirements

The privacy policy page (hosted at `/privacy`) must disclose:

1. **Data controller:** Company name, address, contact email, DPO contact (if applicable)
2. **Data collected:** IP address (hashed), email (for registered users), click data (90-day retention)
3. **Purpose of processing:** URL shortening service, click analytics, abuse prevention
4. **Legal basis:** Contract (registered users), legitimate interest (analytics, security)
5. **Third-party sharing:**
   - Google (OAuth authentication, Safe Browsing API)
   - AWS (infrastructure provider, DPA in place)
6. **Data retention periods:** As specified in Section 1.1
7. **User rights:** Access, erasure, portability, rectification, restriction
8. **Contact for data requests:** dedicated email (e.g., privacy@shorty.io)
9. **Cookie usage:** Strictly necessary cookies only, no consent required
10. **Cross-border transfers:** Data processed in AWS US regions (or EU, depending on deployment). Adequacy decision / SCCs as applicable.

---

## 7. Cross-Border Data Transfers

### 7.1 Current Architecture (US-based)

| Data Flow | From | To | Legal Mechanism |
|---|---|---|---|
| User -> CloudFront | User's country | Nearest edge (global) | N/A (user-initiated) |
| CloudFront -> API Gateway | Edge location | us-east-1 | AWS internal transfer (SCCs) |
| DynamoDB Global Tables | us-east-1 | us-west-2 | AWS internal transfer (same controller) |

### 7.2 EU Deployment Option

For GDPR-strict compliance, deploy the stack in `eu-west-1`:

```hcl
# deploy/terraform/environments/eu-prod/main.tf
module "shorty" {
  source = "../../modules"
  region = "eu-west-1"
  # DynamoDB Global Tables replica in eu-central-1
  replica_regions = ["eu-central-1"]
}
```

This ensures all PII processing occurs within the EU. CloudFront edge locations outside the EU handle only TLS termination and caching of non-PII responses (redirects).

---

## 8. Privacy Impact Assessment (PIA) Summary

| Risk | Likelihood | Impact | Mitigation | Residual Risk |
|---|---|---|---|---|
| IP address exposure via logs | Medium | Medium | 14-day log retention, restricted IAM | Low |
| IP correlation across rotation boundaries | Low | Low | Salt rotates every 90 days, click TTL 90 days | Minimal |
| Email breach via DynamoDB | Low | High | KMS encryption, IAM least privilege | Low |
| Click pattern analysis revealing user behavior | Medium | Medium | Pseudonymized data, 90-day TTL | Low |
| GDPR data deletion incomplete | Low | High | Automated deletion pipeline, verification step | Low |
| Cross-border transfer challenge | Low | Medium | EU deployment option, AWS DPA + SCCs | Low |

---

## 9. Compliance Checklist

- [ ] Privacy policy published at `/privacy`
- [ ] `DELETE /api/v1/me` endpoint implemented and tested
- [ ] `GET /api/v1/me/export` endpoint implemented and tested
- [ ] DynamoDB TTL enabled on `clicks` table (`created_at` + 90 days)
- [ ] CloudWatch Logs retention set to 14 days for all Lambda log groups
- [ ] CloudFront access logs S3 lifecycle policy: 14-day expiration
- [ ] IP hashing verified: no raw IPs in DynamoDB `links` or `clicks` tables
- [ ] User-Agent hashing verified: no raw User-Agent strings in storage
- [ ] Cookie flags verified: httpOnly, Secure, SameSite=Strict
- [ ] AWS DPA confirmed (AWS Customer Agreement)
- [ ] Google OAuth scopes limited to `email` and `profile`
- [ ] Data subject rights response process documented and tested
- [ ] Salt rotation tested: old hashes unlinkable after rotation

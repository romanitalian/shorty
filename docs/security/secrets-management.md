# Secrets Management -- Shorty URL Shortener

**Version:** 1.0
**Date:** 2026-04-05
**Author:** Security Engineer (S1-T05)
**References:** security-architecture.md, iam-policies.md, threat-model.md

---

## 1. KMS Key Hierarchy

All encryption in Shorty flows from a single AWS KMS Customer Managed Key (CMK). Data keys are generated per-service by AWS managed integrations (envelope encryption).

```
AWS KMS Customer Managed Key (CMK)
  Alias: alias/shorty-${env}-master
  Key spec: SYMMETRIC_DEFAULT (AES-256-GCM)
  Multi-region: us-east-1 (primary) + us-west-2 (replica)
  Key rotation: automatic annual rotation (AWS-managed)
  |
  +-- DynamoDB SSE
  |     shorty-links table encryption
  |     shorty-clicks table encryption
  |     shorty-users table encryption
  |
  +-- SQS SSE
  |     shorty-clicks.fifo queue encryption
  |
  +-- Secrets Manager
  |     All secrets encrypted with this CMK
  |
  +-- CloudWatch Logs
        Log group encryption for all Lambda log groups
```

### 1.1 KMS Key Policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "RootAccountFullAccess",
      "Effect": "Allow",
      "Principal": { "AWS": "arn:aws:iam::${account_id}:root" },
      "Action": "kms:*",
      "Resource": "*"
    },
    {
      "Sid": "AdminKeyManagement",
      "Effect": "Allow",
      "Principal": { "AWS": "arn:aws:iam::${account_id}:role/shorty-admin" },
      "Action": [
        "kms:Create*",
        "kms:Describe*",
        "kms:Enable*",
        "kms:List*",
        "kms:Put*",
        "kms:Update*",
        "kms:Revoke*",
        "kms:Disable*",
        "kms:Get*",
        "kms:Delete*",
        "kms:TagResource",
        "kms:UntagResource",
        "kms:ScheduleKeyDeletion",
        "kms:CancelKeyDeletion"
      ],
      "Resource": "*"
    },
    {
      "Sid": "LambdaDecryptViaSecretsManager",
      "Effect": "Allow",
      "Principal": {
        "AWS": [
          "arn:aws:iam::${account_id}:role/shorty-redirect-lambda-role",
          "arn:aws:iam::${account_id}:role/shorty-api-lambda-role"
        ]
      },
      "Action": [
        "kms:Decrypt",
        "kms:DescribeKey"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "kms:ViaService": "secretsmanager.${region}.amazonaws.com"
        }
      }
    },
    {
      "Sid": "DynamoDBEncryption",
      "Effect": "Allow",
      "Principal": { "AWS": "*" },
      "Action": [
        "kms:Decrypt",
        "kms:DescribeKey",
        "kms:Encrypt",
        "kms:ReEncrypt*",
        "kms:GenerateDataKey*"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "kms:CallerAccount": "${account_id}",
          "kms:ViaService": "dynamodb.${region}.amazonaws.com"
        }
      }
    },
    {
      "Sid": "SQSEncryption",
      "Effect": "Allow",
      "Principal": { "AWS": "*" },
      "Action": [
        "kms:Decrypt",
        "kms:GenerateDataKey"
      ],
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "kms:CallerAccount": "${account_id}",
          "kms:ViaService": "sqs.${region}.amazonaws.com"
        }
      }
    },
    {
      "Sid": "SecretsManagerRotationLambda",
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::${account_id}:role/shorty-secret-rotation-lambda-role"
      },
      "Action": [
        "kms:Decrypt",
        "kms:DescribeKey",
        "kms:Encrypt",
        "kms:GenerateDataKey"
      ],
      "Resource": "*"
    }
  ]
}
```

### 1.2 Terraform for KMS

```hcl
# deploy/terraform/modules/kms/main.tf

resource "aws_kms_key" "master" {
  description             = "Shorty ${var.env} master encryption key"
  deletion_window_in_days = 30
  enable_key_rotation     = true
  multi_region            = true

  tags = {
    Project     = "shorty"
    Environment = var.env
  }
}

resource "aws_kms_alias" "master" {
  name          = "alias/shorty-${var.env}-master"
  target_key_id = aws_kms_key.master.key_id
}
```

---

## 2. Secrets Inventory

All secrets are stored in AWS Secrets Manager, encrypted with the project CMK.

| Secret Name | Contents | Type | Rotation Period | Consumers |
|---|---|---|---|---|
| `shorty/${env}/ip-hash-salt` | 32-byte random hex string | SecretString | 90 days (quarterly) | Redirect Lambda, API Lambda |
| `shorty/${env}/redis-auth` | Redis AUTH token (64-char alphanumeric) | SecretString | 180 days | Redirect Lambda, API Lambda |
| `shorty/${env}/csrf-key` | 32-byte HMAC key (hex-encoded) | SecretString | 30 days | API Lambda |
| `shorty/${env}/safe-browsing-api-key` | Google Safe Browsing API key | SecretString | Annual (manual) | API Lambda |
| `shorty/${env}/cognito` | `{"client_id":"...","client_secret":"..."}` | SecretString (JSON) | Manual (Cognito-managed) | API Lambda |
| `shorty/${env}/google-oauth` | `{"client_id":"...","client_secret":"..."}` | SecretString (JSON) | Manual (Google Console) | Cognito (federated IdP) |

### 2.1 Access Control Matrix

| Secret | Redirect Lambda | API Lambda | Worker Lambda | Rotation Lambda | Admin |
|---|---|---|---|---|---|
| `ip-hash-salt` | Read | Read | -- | Read/Write | Full |
| `redis-auth` | Read | Read | -- | Read/Write | Full |
| `csrf-key` | -- | Read | -- | Read/Write | Full |
| `safe-browsing-api-key` | -- | Read | -- | -- | Full |
| `cognito` | -- | Read | -- | -- | Full |
| `google-oauth` | -- | -- | -- | -- | Full |

**Enforcement:** IAM policies (see `docs/aws/iam-policies.md`) restrict `secretsmanager:GetSecretValue` to specific ARN patterns per Lambda role. Worker Lambda has zero Secrets Manager access.

---

## 3. Rotation Strategy

### 3.1 Automatic Rotation via Lambda

Each rotatable secret has a Secrets Manager rotation configuration backed by a dedicated rotation Lambda function.

```
Secrets Manager
  |
  +-- RotationSchedule (cron)
  |     |
  |     +-- Invokes shorty-secret-rotation Lambda
  |           |
  |           +-- Step 1: createSecret  (generate new value, store as AWSPENDING)
  |           +-- Step 2: setSecret     (update dependent service if needed)
  |           +-- Step 3: testSecret    (verify new secret works)
  |           +-- Step 4: finishSecret  (promote AWSPENDING -> AWSCURRENT)
```

### 3.2 IP Hash Salt Rotation (90-day cycle)

**Behavior on rotation:** Old hashed IPs in the `clicks` table become permanently unlinkable from the new salt. This is a deliberate privacy feature -- click analytics naturally disconnect across rotation boundaries. No migration of old hashes is performed.

**Dual-read window:** During the rotation window (up to 60 minutes), Lambdas that fail to retrieve the `AWSCURRENT` version will fall back to re-fetching. The rotation Lambda:
1. Generates a 32-byte cryptographically random value: `crypto/rand.Read(buf)`
2. Stores as `AWSPENDING`
3. No external service update needed (salt is used only in application code)
4. Tests by calling `GetSecretValue` with `AWSPENDING` staging label
5. Promotes to `AWSCURRENT`

```go
// Rotation Lambda handler for ip-hash-salt
func handleRotation(ctx context.Context, event SecretsManagerRotationEvent) error {
    client := secretsmanager.NewFromConfig(cfg)

    switch event.Step {
    case "createSecret":
        buf := make([]byte, 32)
        if _, err := rand.Read(buf); err != nil {
            return fmt.Errorf("failed to generate random salt: %w", err)
        }
        newSalt := hex.EncodeToString(buf)

        _, err := client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
            SecretId:           aws.String(event.SecretId),
            ClientRequestToken: aws.String(event.ClientRequestToken),
            SecretString:       aws.String(newSalt),
            VersionStages:      []string{"AWSPENDING"},
        })
        return err

    case "setSecret":
        // No external service to update for IP salt
        return nil

    case "testSecret":
        // Verify we can read the pending version
        _, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
            SecretId:     aws.String(event.SecretId),
            VersionStage: aws.String("AWSPENDING"),
        })
        return err

    case "finishSecret":
        // Promote AWSPENDING to AWSCURRENT
        _, err := client.UpdateSecretVersionStage(ctx, &secretsmanager.UpdateSecretVersionStageInput{
            SecretId:            aws.String(event.SecretId),
            VersionStage:        aws.String("AWSCURRENT"),
            MoveToVersionId:     aws.String(event.ClientRequestToken),
            RemoveFromVersionId: aws.String(getCurrentVersionId(ctx, client, event.SecretId)),
        })
        return err
    }
    return fmt.Errorf("unknown rotation step: %s", event.Step)
}
```

### 3.3 Redis AUTH Token Rotation (180-day cycle)

**Critical:** Redis AUTH rotation requires coordinating the token change with ElastiCache. The rotation Lambda must:
1. Generate new 64-char token
2. Call ElastiCache `ModifyReplicationGroup` to set the new AUTH token
3. Wait for ElastiCache to apply the change (can take several minutes)
4. Verify connectivity with the new token
5. Promote the new secret version

During rotation, ElastiCache supports dual AUTH tokens (old + new) for a configurable window.

```hcl
# Terraform: rotation schedule for Redis AUTH
resource "aws_secretsmanager_secret_rotation" "redis_auth" {
  secret_id           = aws_secretsmanager_secret.redis_auth.id
  rotation_lambda_arn = aws_lambda_function.secret_rotation.arn

  rotation_rules {
    automatically_after_days = 180
  }
}
```

### 3.4 CSRF Key Rotation (30-day cycle)

**Dual-key validation:** During the 1-hour rotation window, the application accepts CSRF tokens signed with either the old or new key. Implementation:

```go
var (
    csrfKeyCurrent  []byte
    csrfKeyPrevious []byte
)

// LoadCSRFKeys fetches both AWSCURRENT and AWSPREVIOUS versions.
func LoadCSRFKeys(ctx context.Context, client *secretsmanager.Client) error {
    current, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
        SecretId:     aws.String("shorty/prod/csrf-key"),
        VersionStage: aws.String("AWSCURRENT"),
    })
    if err != nil {
        return err
    }
    csrfKeyCurrent, _ = hex.DecodeString(aws.ToString(current.SecretString))

    previous, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
        SecretId:     aws.String("shorty/prod/csrf-key"),
        VersionStage: aws.String("AWSPREVIOUS"),
    })
    if err == nil {
        csrfKeyPrevious, _ = hex.DecodeString(aws.ToString(previous.SecretString))
    }
    return nil
}

// ValidateCSRFToken tries current key first, then previous key.
func ValidateCSRFToken(token, sessionID string) error {
    if err := validateWithKey(token, sessionID, csrfKeyCurrent); err == nil {
        return nil
    }
    if csrfKeyPrevious != nil {
        return validateWithKey(token, sessionID, csrfKeyPrevious)
    }
    return fmt.Errorf("CSRF token validation failed")
}
```

---

## 4. Secret Versioning and Rollback

### 4.1 Version Stages

Secrets Manager maintains three version stages per secret:

| Stage | Description | Use Case |
|---|---|---|
| `AWSCURRENT` | Active production version | Normal reads |
| `AWSPENDING` | Being validated during rotation | Rotation Lambda only |
| `AWSPREVIOUS` | Previous AWSCURRENT (auto-maintained) | Rollback, dual-key validation |

### 4.2 Rollback Procedure

If a rotation causes issues, rollback is immediate:

```bash
# 1. Identify the previous version ID
aws secretsmanager list-secret-version-ids \
  --secret-id "shorty/prod/ip-hash-salt" \
  --query 'Versions[?VersionStages[?contains(@,`AWSPREVIOUS`)]].VersionId' \
  --output text

# 2. Promote AWSPREVIOUS back to AWSCURRENT
aws secretsmanager update-secret-version-stage \
  --secret-id "shorty/prod/ip-hash-salt" \
  --version-stage "AWSCURRENT" \
  --move-to-version-id "<previous-version-id>" \
  --remove-from-version-id "<current-version-id>"

# 3. Verify the rollback
aws secretsmanager get-secret-value \
  --secret-id "shorty/prod/ip-hash-salt" \
  --query 'VersionId'
```

**Lambda cache invalidation:** After rollback, Lambda instances holding the old secret in memory will continue using it until the next cold start. Force refresh by:
- Publishing a dummy update to the Lambda function (triggers cold starts for new invocations)
- Or waiting for natural cold start rotation (typically < 30 minutes under normal traffic)

---

## 5. Secret Injection into Lambda

Secrets are fetched at Lambda cold start (outside the handler function) and cached in package-level variables. This avoids Secrets Manager API calls on every invocation.

### 5.1 Lambda Environment Variables

Lambda environment variables reference the secret name, not the secret value:

```hcl
# deploy/terraform/modules/lambda/main.tf

resource "aws_lambda_function" "redirect" {
  function_name = "shorty-redirect"
  # ...

  environment {
    variables = {
      SECRET_IP_SALT_NAME    = aws_secretsmanager_secret.ip_salt.name
      SECRET_REDIS_AUTH_NAME = aws_secretsmanager_secret.redis_auth.name
      ENV                    = var.env
      # NEVER put actual secret values here
    }
  }
}
```

### 5.2 Cold-Start Secret Loading

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
    ipSalt    string
    redisAuth string
)

func init() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    cfg, err := config.LoadDefaultConfig(ctx)
    if err != nil {
        panic(fmt.Sprintf("failed to load AWS config: %v", err))
    }

    client := secretsmanager.NewFromConfig(cfg)

    ipSalt = mustGetSecret(ctx, client, os.Getenv("SECRET_IP_SALT_NAME"))
    redisAuth = mustGetSecret(ctx, client, os.Getenv("SECRET_REDIS_AUTH_NAME"))
}

func mustGetSecret(ctx context.Context, client *secretsmanager.Client, name string) string {
    out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
        SecretId: aws.String(name),
    })
    if err != nil {
        panic(fmt.Sprintf("failed to get secret %q: %v", name, err))
    }
    return aws.ToString(out.SecretString)
}
```

### 5.3 Secret Refresh on Version Change

If a Lambda encounters a `SecretVersionNotFound` error (e.g., after rotation), it should re-fetch:

```go
func getSecretWithRefresh(ctx context.Context, client *secretsmanager.Client, name string, cached *string) (string, error) {
    if *cached != "" {
        return *cached, nil
    }
    val := mustGetSecret(ctx, client, name)
    *cached = val
    return val, nil
}
```

---

## 6. Local Development Secrets

### 6.1 `.env` File (Never Committed)

Local development uses a `.env` file loaded by Docker Compose and the Go application:

```bash
# .env -- LOCAL DEVELOPMENT ONLY -- NEVER COMMIT
# Copy from .env.example and fill in values

IP_HASH_SALT=local-dev-salt-not-for-production
REDIS_AUTH_TOKEN=local-redis-token
CSRF_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
SAFE_BROWSING_API_KEY=
COGNITO_CLIENT_ID=local-client-id
COGNITO_CLIENT_SECRET=local-client-secret
```

### 6.2 `.env.example` (Committed -- Template)

```bash
# .env.example -- Copy to .env and fill in values
# DO NOT put real secrets here

IP_HASH_SALT=change-me-local-salt
REDIS_AUTH_TOKEN=change-me-local-token
CSRF_KEY=change-me-64-hex-chars
SAFE_BROWSING_API_KEY=
COGNITO_CLIENT_ID=
COGNITO_CLIENT_SECRET=
```

### 6.3 LocalStack Secrets Manager

For integration tests, secrets are provisioned in LocalStack:

```bash
# deploy/scripts/localstack-init.sh

awslocal secretsmanager create-secret \
  --name "shorty/local/ip-hash-salt" \
  --secret-string "local-dev-salt-not-for-production"

awslocal secretsmanager create-secret \
  --name "shorty/local/redis-auth" \
  --secret-string "local-redis-token"

awslocal secretsmanager create-secret \
  --name "shorty/local/csrf-key" \
  --secret-string "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

---

## 7. `.gitignore` Patterns for Secret Files

The following patterns must be present in the project `.gitignore`:

```gitignore
# Secrets and environment files
.env
.env.local
.env.*.local
*.pem
*.key
*.p12
*.pfx
credentials.json
secrets.json
service-account.json

# Terraform state (contains secret values)
*.tfstate
*.tfstate.*
.terraform/

# IDE files that may cache secrets
.idea/
.vscode/settings.json

# OS files
.DS_Store
```

---

## 8. Incident Response: Secret Compromise Procedure

### 8.1 Triage (0-15 minutes)

1. **Identify the compromised secret** from the alert (CloudTrail unusual access, public exposure, etc.)
2. **Assess blast radius:** Which Lambdas use this secret? What data does it protect?
3. **Classify severity:**
   - `ip-hash-salt`: Medium -- attacker can correlate hashed IPs (privacy impact, not auth)
   - `redis-auth`: High -- attacker can read/write cache, bypass rate limits
   - `csrf-key`: Medium -- attacker can forge CSRF tokens for password forms
   - `cognito` secrets: Critical -- attacker can impersonate OAuth flows
   - `safe-browsing-api-key`: Low -- cost impact only

### 8.2 Rotate (15-30 minutes)

```bash
# Force immediate rotation (bypasses schedule)
aws secretsmanager rotate-secret \
  --secret-id "shorty/prod/<secret-name>" \
  --rotation-lambda-arn "arn:aws:lambda:${region}:${account_id}:function:shorty-secret-rotation"

# Verify rotation completed
aws secretsmanager describe-secret \
  --secret-id "shorty/prod/<secret-name>" \
  --query 'RotationEnabled'
```

For secrets without automatic rotation (Cognito, Google OAuth):

```bash
# 1. Generate new credentials in the respective console
# 2. Update in Secrets Manager
aws secretsmanager put-secret-value \
  --secret-id "shorty/prod/cognito" \
  --secret-string '{"client_id":"NEW_ID","client_secret":"NEW_SECRET"}'

# 3. Update Cognito app client configuration
aws cognito-idp update-user-pool-client \
  --user-pool-id "${user_pool_id}" \
  --client-id "${new_client_id}" \
  # ... remaining config
```

### 8.3 Audit (30-60 minutes)

```bash
# Search CloudTrail for unauthorized access to the compromised secret
aws cloudtrail lookup-events \
  --lookup-attributes AttributeKey=ResourceName,AttributeValue="arn:aws:secretsmanager:${region}:${account_id}:secret:shorty/prod/<secret-name>" \
  --start-time "$(date -u -d '7 days ago' '+%Y-%m-%dT%H:%M:%SZ')" \
  --max-results 50
```

Review for:
- Access from unexpected IAM principals
- Access from unexpected source IPs
- Access volume anomalies

### 8.4 Notify (within 1 hour)

- Internal: Engineering team via incident channel
- If PII was exposed (ip-hash-salt compromise): Privacy officer within 24 hours
- If user credentials affected: Users via email within 72 hours (GDPR requirement)

### 8.5 Post-Incident

- Update this document with lessons learned
- Add CloudWatch alarm if the detection gap was too long
- Create ADR documenting the incident and response

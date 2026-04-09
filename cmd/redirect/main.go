package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/romanitalian/shorty/internal/cache"
	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/store"
)

const (
	// rateLimitRedirectLimit is the max requests per IP per window for anonymous redirects.
	rateLimitRedirectLimit int64 = 200
	// rateLimitRedirectWindow is the sliding window duration for redirect rate limiting.
	rateLimitRedirectWindow = 1 * time.Minute
	// sqsPublishTimeout is the max time allowed for async SQS click event publishing.
	sqsPublishTimeout = 2 * time.Second
	// clickDataTTLDays is the TTL for click records in DynamoDB (90 days).
	clickDataTTLDays = 90
)

// csrfTokenMaxAge is the maximum age of a CSRF token in seconds (10 minutes).
const csrfTokenMaxAge = 600

// passwordFormHTML is the HTML template for password-protected links.
const passwordFormHTML = `<!DOCTYPE html>
<html>
<head><title>Password Required</title></head>
<body>
<h1>This link is password-protected</h1>
<form method="POST" action="/{{.Code}}">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<label for="password">Password:</label>
<input type="password" id="password" name="password" required>
<button type="submit">Submit</button>
</form>
</body>
</html>`

var passwordTmpl = template.Must(template.New("password").Parse(passwordFormHTML))

// SQSPublisher abstracts SQS message sending for testability.
type SQSPublisher interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// RedirectHandler holds all dependencies for the redirect Lambda.
type RedirectHandler struct {
	store      store.Store
	cache      cache.Cache
	limiter    ratelimit.Limiter
	sqsPub     SQSPublisher
	queueURL   string
	ipSalt     string
	csrfSecret string
	nowFunc    func() time.Time
}

// NewRedirectHandler creates a new handler with all dependencies injected.
func NewRedirectHandler(
	st store.Store,
	ca cache.Cache,
	lim ratelimit.Limiter,
	sqsPub SQSPublisher,
	queueURL string,
	ipSalt string,
	csrfSecret string,
) *RedirectHandler {
	return &RedirectHandler{
		store:      st,
		cache:      ca,
		limiter:    lim,
		sqsPub:     sqsPub,
		queueURL:   queueURL,
		ipSalt:     ipSalt,
		csrfSecret: csrfSecret,
		nowFunc:    time.Now,
	}
}

// HashIP computes SHA-256(salt + ip) and returns the hex-encoded hash.
func HashIP(ip string, salt string) string {
	h := sha256.New()
	h.Write([]byte(salt))
	h.Write([]byte(ip))
	return hex.EncodeToString(h.Sum(nil))
}

// Handle processes a redirect request from API Gateway v2.
func (h *RedirectHandler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	// 1. Extract code from path
	code := extractCode(req.RawPath)
	if code == "" {
		return jsonResponse(http.StatusBadRequest, `{"error":"missing short code"}`), nil
	}

	// Extract client IP
	clientIP := req.RequestContext.HTTP.SourceIP
	if clientIP == "" {
		clientIP = req.Headers["x-forwarded-for"]
		if idx := strings.Index(clientIP, ","); idx != -1 {
			clientIP = strings.TrimSpace(clientIP[:idx])
		}
	}
	ipHash := HashIP(clientIP, h.ipSalt)

	// 2. Rate limit check — BEFORE any DB operation
	rlKey := fmt.Sprintf("rl:redirect:%s", ipHash)
	rlResult, err := h.limiter.Allow(ctx, rlKey, rateLimitRedirectLimit, rateLimitRedirectWindow)
	if err != nil {
		// Rate limiter error but FailOpen policy should still return allowed=true.
		// If we got an error with allowed=false, respect it.
		if rlResult != nil && !rlResult.Allowed {
			return rateLimitResponse(rlResult), nil
		}
	}
	if rlResult != nil && !rlResult.Allowed {
		return rateLimitResponse(rlResult), nil
	}

	// 3. Cache check (Redis)
	link, _ := h.cache.GetLink(ctx, code)
	cacheHit := link != nil

	if !cacheHit {
		// 4. Negative cache check
		neg, _ := h.cache.IsNegative(ctx, code)
		if neg {
			return jsonResponse(http.StatusNotFound, `{"error":"link not found"}`), nil
		}

		// 5. DynamoDB fallback
		link, err = h.store.GetLink(ctx, code)
		if err != nil {
			if errors.Is(err, store.ErrLinkNotFound) {
				// Set negative cache (fire-and-forget)
				_ = h.cache.SetNegative(ctx, code)
				return jsonResponse(http.StatusNotFound, `{"error":"link not found"}`), nil
			}
			return jsonResponse(http.StatusInternalServerError, `{"error":"internal error"}`), nil
		}

		// 6. Cache warm (fire-and-forget)
		_ = h.cache.SetLink(ctx, code, link, 0)
	}

	now := h.now()

	// 7. TTL check
	if link.ExpiresAt != nil && *link.ExpiresAt > 0 && time.Unix(*link.ExpiresAt, 0).Before(now) {
		return jsonResponse(http.StatusGone, `{"error":"link has expired"}`), nil
	}

	// 8. Active check
	if !link.IsActive {
		return jsonResponse(http.StatusGone, `{"error":"link is no longer active"}`), nil
	}

	// 9. Password check
	if link.PasswordHash != "" {
		// Check if password was submitted via POST
		if req.RequestContext.HTTP.Method == http.MethodPost {
			submittedPassword := ""
			csrfToken := ""
			if req.Headers["content-type"] == "application/x-www-form-urlencoded" {
				vals, parseErr := url.ParseQuery(req.Body)
				if parseErr == nil {
					submittedPassword = vals.Get("password")
					csrfToken = vals.Get("csrf_token")
				}
			}

			// Validate CSRF token before password comparison (B2)
			if !h.validateCSRFToken(csrfToken, code) {
				return h.passwordFormResponseWithCSRF(code), nil
			}

			// Use bcrypt for password comparison (B1)
			if err := bcrypt.CompareHashAndPassword([]byte(link.PasswordHash), []byte(submittedPassword)); err != nil {
				return h.passwordFormResponseWithCSRF(code), nil
			}
			// Password correct, continue to redirect
		} else {
			// Show password form with CSRF token
			return h.passwordFormResponseWithCSRF(code), nil
		}
	}

	// 10. Click count check + increment
	ok, err := h.store.IncrementClickCount(ctx, code, link.MaxClicks)
	if err != nil {
		return jsonResponse(http.StatusInternalServerError, `{"error":"internal error"}`), nil
	}
	if !ok {
		return jsonResponse(http.StatusGone, `{"error":"click limit reached"}`), nil
	}

	// 11. Async SQS click event — fire goroutine, NEVER block the redirect
	h.publishClickEventAsync(code, ipHash, req)

	// 12. Build redirect URL with UTM params
	redirectURL := buildRedirectURL(link)

	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusFound,
		Headers: mergeHeaders(map[string]string{
			"Location":      redirectURL,
			"Cache-Control": "no-store, no-cache, must-revalidate, private",
		}),
	}, nil
}

// extractCode parses the short code from the request path.
// Expects paths like "/{code}" or "/abc123".
func extractCode(rawPath string) string {
	path := strings.TrimPrefix(rawPath, "/")
	// Remove any trailing slash or query params that may have leaked
	if idx := strings.IndexAny(path, "/?"); idx != -1 {
		path = path[:idx]
	}
	return path
}

// now returns the current time, using the injectable nowFunc.
func (h *RedirectHandler) now() time.Time {
	if h.nowFunc != nil {
		return h.nowFunc()
	}
	return time.Now()
}

// publishClickEventAsync sends a click event to SQS in a goroutine with a timeout.
func (h *RedirectHandler) publishClickEventAsync(code, ipHash string, req events.APIGatewayV2HTTPRequest) {
	if h.sqsPub == nil || h.queueURL == "" {
		return
	}

	userAgent := req.Headers["user-agent"]
	referer := req.Headers["referer"]

	go func() {
		sqsCtx, cancel := context.WithTimeout(context.Background(), sqsPublishTimeout)
		defer cancel()

		eventID := uuid.New().String()
		now := time.Now()

		clickEvent := map[string]interface{}{
			"code":           code,
			"ip_hash":        ipHash,
			"user_agent":     userAgent,
			"referer_domain": extractDomain(referer),
			"timestamp":      now.Unix(),
		}

		body, err := json.Marshal(clickEvent)
		if err != nil {
			return
		}

		_, _ = h.sqsPub.SendMessage(sqsCtx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(h.queueURL),
			MessageBody:            aws.String(string(body)),
			MessageGroupId:         aws.String(code),
			MessageDeduplicationId: aws.String(eventID),
			MessageAttributes: map[string]sqstypes.MessageAttributeValue{
				"event_type": {
					DataType:    aws.String("String"),
					StringValue: aws.String("click"),
				},
			},
		})
	}()
}

// extractDomain returns just the host from a URL, or the raw string if parsing fails.
func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}

// buildRedirectURL appends UTM parameters to the original URL if configured.
func buildRedirectURL(link *store.Link) string {
	if link.UTMSource == "" && link.UTMMedium == "" && link.UTMCampaign == "" {
		return link.OriginalURL
	}

	u, err := url.Parse(link.OriginalURL)
	if err != nil {
		return link.OriginalURL
	}

	q := u.Query()
	if link.UTMSource != "" {
		q.Set("utm_source", link.UTMSource)
	}
	if link.UTMMedium != "" {
		q.Set("utm_medium", link.UTMMedium)
	}
	if link.UTMCampaign != "" {
		q.Set("utm_campaign", link.UTMCampaign)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// securityHeaders returns the standard security headers applied to every response.
func securityHeaders() map[string]string {
	return map[string]string{
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
	}
}

// mergeHeaders copies security headers and then applies additional headers.
func mergeHeaders(extra map[string]string) map[string]string {
	h := securityHeaders()
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// jsonResponse builds a JSON API Gateway response.
func jsonResponse(status int, body string) events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers: mergeHeaders(map[string]string{
			"Content-Type": "application/json",
		}),
		Body: body,
	}
}

// rateLimitResponse builds a 429 response with standard rate limit headers.
func rateLimitResponse(result *ratelimit.Result) events.APIGatewayV2HTTPResponse {
	retryAfterSec := int(result.RetryAfter.Seconds())
	if retryAfterSec < 1 {
		retryAfterSec = 1
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusTooManyRequests,
		Headers: mergeHeaders(map[string]string{
			"Content-Type":          "application/json",
			"Retry-After":           fmt.Sprintf("%d", retryAfterSec),
			"X-RateLimit-Limit":     fmt.Sprintf("%d", result.Limit),
			"X-RateLimit-Reset":     fmt.Sprintf("%d", result.ResetAt.Unix()),
			"X-RateLimit-Remaining": fmt.Sprintf("%d", result.Remaining),
		}),
		Body: `{"error":"rate limit exceeded"}`,
	}
}

// generateCSRFToken creates an HMAC-SHA256 CSRF token from (secret + code + timestamp).
func (h *RedirectHandler) generateCSRFToken(code string) string {
	ts := strconv.FormatInt(h.now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(h.csrfSecret))
	mac.Write([]byte(code))
	mac.Write([]byte(ts))
	sig := hex.EncodeToString(mac.Sum(nil))
	return ts + "." + sig
}

// validateCSRFToken validates the CSRF token for the given code.
func (h *RedirectHandler) validateCSRFToken(token, code string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	tsStr, sig := parts[0], parts[1]

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false
	}

	// Check expiry
	if h.now().Unix()-ts > csrfTokenMaxAge {
		return false
	}

	// Recompute HMAC
	mac := hmac.New(sha256.New, []byte(h.csrfSecret))
	mac.Write([]byte(code))
	mac.Write([]byte(tsStr))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// passwordFormResponseWithCSRF renders the HTML password form with a CSRF token.
func (h *RedirectHandler) passwordFormResponseWithCSRF(code string) events.APIGatewayV2HTTPResponse {
	csrfToken := h.generateCSRFToken(code)
	var buf strings.Builder
	_ = passwordTmpl.Execute(&buf, struct {
		Code      string
		CSRFToken string
	}{Code: code, CSRFToken: csrfToken})
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusForbidden,
		Headers: mergeHeaders(map[string]string{
			"Content-Type":            "text/html; charset=utf-8",
			"Content-Security-Policy": "default-src 'none'; form-action 'self'; style-src 'unsafe-inline'",
		}),
		Body: buf.String(),
	}
}

// --- Lambda initialization (outside handler, survives warm starts) ---

var redirectHandler *RedirectHandler

func init() {
	ctx := context.Background()

	// Load AWS config
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to load AWS config: %v", err))
	}

	// Initialize DynamoDB store
	dynamoClient := dynamodb.NewFromConfig(cfg)
	linksTable := envOrDefault("LINKS_TABLE", "shorty-links")
	clicksTable := envOrDefault("CLICKS_TABLE", "shorty-clicks")
	usersTable := envOrDefault("USERS_TABLE", "shorty-users")
	dynamoStore := store.NewDynamoStore(dynamoClient, linksTable, clicksTable, usersTable)

	// Initialize Redis cache + rate limiter
	redisAddr := envOrDefault("REDIS_ADDR", "localhost:6379")
	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     10,
	})
	redisCache := cache.NewRedisCache(redisClient)
	limiter := ratelimit.NewRedisLimiter(redisClient, ratelimit.WithFailurePolicy(ratelimit.FailOpen))

	// Initialize SQS client
	sqsClient := sqs.NewFromConfig(cfg)
	queueURL := envOrDefault("CLICKS_QUEUE_URL", "")

	// Load IP salt from Secrets Manager
	ipSalt := os.Getenv("IP_HASH_SALT")
	if ipSalt == "" {
		secretName := envOrDefault("IP_HASH_SALT_SECRET", "shorty/ip-hash-salt")
		smClient := secretsmanager.NewFromConfig(cfg)
		result, err := smClient.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(secretName),
		})
		if err != nil {
			// Fall back to a default salt for local development.
			ipSalt = "local-dev-salt-DO-NOT-USE-IN-PRODUCTION"
		} else {
			ipSalt = aws.ToString(result.SecretString)
		}
	}

	// Load CSRF secret (reuse IP salt derivation pattern; separate secret in production).
	csrfSecret := os.Getenv("CSRF_SECRET")
	if csrfSecret == "" {
		csrfSecret = "csrf-" + ipSalt // derive from IP salt if not explicitly set
	}

	redirectHandler = NewRedirectHandler(dynamoStore, redisCache, limiter, sqsClient, queueURL, ipSalt, csrfSecret)
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return redirectHandler.Handle(ctx, req)
}

func main() {
	lambda.Start(handler)
}

// envOrDefault reads an env var with a fallback default value.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

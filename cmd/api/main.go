package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/auth"
	"github.com/romanitalian/shorty/internal/cache"
	mw "github.com/romanitalian/shorty/internal/middleware"
	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/shortener"
	"github.com/romanitalian/shorty/internal/store"
	"github.com/romanitalian/shorty/internal/validator"
)

var (
	router     http.Handler
	chiAdapter *chiadapter.ChiLambdaV2
)

func init() {
	ctx := context.Background()

	// AWS SDK config.
	opts := []func(*config.LoadOptions) error{}
	if ep := os.Getenv("AWS_ENDPOINT_URL"); ep != "" {
		opts = append(opts, config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: ep, SigningRegion: region}, nil
			}),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		panic(fmt.Sprintf("unable to load AWS config: %v", err))
	}

	// DynamoDB.
	ddbClient := dynamodb.NewFromConfig(cfg)
	linksTable := envOrDefault("LINKS_TABLE", "links")
	clicksTable := envOrDefault("CLICKS_TABLE", "clicks")
	usersTable := envOrDefault("USERS_TABLE", "users")
	apiStore := store.NewDynamoStore(ddbClient, linksTable, clicksTable, usersTable)

	// Redis.
	redisAddr := envOrDefault("REDIS_ADDR", "localhost:6379")
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	apiCache := cache.NewRedisCache(rdb)
	limiter := ratelimit.NewRedisLimiter(rdb, ratelimit.WithFailurePolicy(ratelimit.FailClosed))

	// Code generator.
	gen_ := shortener.New(apiStore)

	// URL validator (M1: enable DNS check by default).
	urlValidator := validator.New(validator.WithDNSCheck())

	// Authenticator.
	var authenticator auth.Authenticator
	if os.Getenv("LOCAL_MODE") == "true" {
		authenticator = &localAuthenticator{}
	} else {
		authenticator = auth.NewCognitoAuthenticator(auth.CognitoConfig{
			Region:     envOrDefault("AWS_REGION", "us-east-1"),
			UserPoolID: os.Getenv("COGNITO_USER_POOL_ID"),
			ClientID:   os.Getenv("COGNITO_CLIENT_ID"),
		})
	}

	// API server.
	srv := NewAPIServer(apiStore, apiCache, limiter, gen_, urlValidator)

	// Chi router.
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(mw.SecurityHeaders)             // B3: security headers on every response
	r.Use(mw.MaxBodySize(10 * 1024))      // B4: 10 KB request body limit
	r.Use(auth.Middleware(authenticator)) // S5: JWT auth middleware

	// Local-only: serve Redoc at /docs and raw spec at /openapi.yaml.
	// These are registered on the same chi router; chi's trie prioritises
	// static segments over the generated /{code} param route, so they
	// don't collide with the redirect handler.
	if os.Getenv("LOCAL_MODE") == "true" {
		specPath := envOrDefault("OPENAPI_SPEC_PATH", "docs/api/openapi.yaml")
		registerDocsHandlers(r, specPath)
	}

	router = gen.HandlerFromMux(srv, r)
	chiAdapter = chiadapter.NewV2(r) // M3: Lambda proxy adapter (API Gateway v2)
}

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return chiAdapter.ProxyWithContextV2(ctx, req)
}

func main() {
	if os.Getenv("LOCAL_MODE") == "true" {
		fmt.Println("Starting API server on :8080")
		if err := http.ListenAndServe(":8080", router); err != nil {
			panic(err)
		}
		return
	}
	lambda.Start(handler)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// localAuthenticator is a mock authenticator for local development that accepts
// any non-empty token and extracts a user ID from the token value itself.
type localAuthenticator struct{}

func (a *localAuthenticator) ValidateToken(_ context.Context, tokenString string) (*auth.Claims, error) {
	return &auth.Claims{
		Subject: tokenString,
		Email:   tokenString + "@local.dev",
	}, nil
}

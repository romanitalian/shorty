package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"

	"github.com/romanitalian/shorty/internal/geo"
	"github.com/romanitalian/shorty/internal/store"
	"github.com/romanitalian/shorty/internal/telemetry"
)

const (
	// clicksTTLDays is the TTL for click records in DynamoDB (90 days).
	clicksTTLDays = 90
)

// sqsClickMessage is the JSON structure published by the redirect Lambda.
type sqsClickMessage struct {
	Code          string `json:"code"`
	IPHash        string `json:"ip_hash"`
	UserAgent     string `json:"user_agent"`
	RefererDomain string `json:"referer_domain"`
	Timestamp     int64  `json:"timestamp"`
}

// Worker holds all dependencies for click event processing.
type Worker struct {
	store       store.Store
	geoResolver geo.Resolver
	clicksTTL   time.Duration
	nowFunc     func() time.Time
}

// NewWorker creates a new Worker with the given dependencies.
func NewWorker(s store.Store, resolver geo.Resolver, ttl time.Duration) *Worker {
	return &Worker{
		store:       s,
		geoResolver: resolver,
		clicksTTL:   ttl,
		nowFunc:     time.Now,
	}
}

// now returns the current time, using the injectable nowFunc.
func (w *Worker) now() time.Time {
	if w.nowFunc != nil {
		return w.nowFunc()
	}
	return time.Now()
}

// HandleSQSEvent processes a batch of SQS click event messages.
// It returns partial batch failures via SQSEventResponse so that only
// failed messages are retried by SQS.
func (w *Worker) HandleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	var failures []events.SQSBatchItemFailure
	var clickEvents []*store.ClickEvent
	// Track which SQS message IDs map to which click events for failure reporting.
	type parsedItem struct {
		messageID  string
		clickEvent *store.ClickEvent
	}
	var parsed []parsedItem

	now := w.now()

	for _, record := range event.Records {
		var msg sqsClickMessage
		if err := json.Unmarshal([]byte(record.Body), &msg); err != nil {
			fmt.Printf("[worker] ERROR parse message %s: %v\n", record.MessageId, err)
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		// Validate required fields.
		if msg.Code == "" || msg.Timestamp == 0 {
			fmt.Printf("[worker] ERROR invalid message %s: missing code or timestamp\n", record.MessageId)
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		// Check if the event is older than the TTL window — skip expired events.
		eventTime := time.Unix(msg.Timestamp, 0)
		if now.Sub(eventTime) > w.clicksTTL {
			fmt.Printf("[worker] WARN skipping expired event %s: timestamp %d is older than %v\n",
				record.MessageId, msg.Timestamp, w.clicksTTL)
			// Not a failure — just skip. The message is too old to be useful.
			continue
		}

		// Geo resolve country from IP (stub returns "XX").
		country := w.geoResolver.Country(ctx, msg.IPHash)
		// Classify device type from user agent.
		deviceType := w.geoResolver.DeviceType(ctx, msg.UserAgent)

		// Build the click event for DynamoDB.
		clickID := uuid.New().String()
		ce := &store.ClickEvent{
			PK:            "LINK#" + msg.Code,
			SK:            fmt.Sprintf("CLICK#%d#%s", msg.Timestamp, clickID),
			IPHash:        msg.IPHash,
			Country:       country,
			DeviceType:    deviceType,
			RefererDomain: msg.RefererDomain,
			UserAgentHash: hashString(msg.UserAgent), // B2 fix: hash the user agent, not the IP
			CreatedAt:     msg.Timestamp,
			TTL:           now.Add(w.clicksTTL).Unix(),
		}

		clickEvents = append(clickEvents, ce)
		parsed = append(parsed, parsedItem{
			messageID:  record.MessageId,
			clickEvent: ce,
		})
	}

	// Batch write all successfully parsed events to DynamoDB.
	if len(clickEvents) > 0 {
		if err := w.store.BatchWriteClicks(ctx, clickEvents); err != nil {
			fmt.Printf("[worker] ERROR BatchWriteClicks: %v\n", err)
			// All items in this batch failed — report them all.
			for _, p := range parsed {
				failures = append(failures, events.SQSBatchItemFailure{
					ItemIdentifier: p.messageID,
				})
			}
		} else {
			fmt.Printf("[worker] wrote %d click events\n", len(clickEvents))
		}
	}

	return events.SQSEventResponse{
		BatchItemFailures: failures,
	}, nil
}

// --- Lambda initialization (outside handler, survives warm starts) ---

var worker *Worker

func init() {
	ctx := context.Background()

	// OTel setup (no-op if OTEL_EXPORTER_OTLP_ENDPOINT is empty).
	shutdownTracer, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "shorty-worker",
		ServiceVersion: "0.1.0",
		Environment:    envOrDefault("ENVIRONMENT", "local"),
		OTLPEndpoint:   os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	})
	if err != nil {
		fmt.Printf("[worker] WARN telemetry init failed: %v\n", err)
	}
	_ = shutdownTracer // shutdown is called by Lambda runtime on process exit

	// AWS SDK config (supports LocalStack via AWS_ENDPOINT_URL).
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		panic(fmt.Sprintf("[worker] failed to load AWS config: %v", err))
	}

	// DynamoDB client + store.
	dynamoClient := dynamodb.NewFromConfig(cfg)
	linksTable := envOrDefault("LINKS_TABLE", "shorty-links")
	clicksTable := envOrDefault("CLICKS_TABLE", "shorty-clicks")
	usersTable := envOrDefault("USERS_TABLE", "shorty-users")
	dynamoStore := store.NewDynamoStore(dynamoClient, linksTable, clicksTable, usersTable)

	// Geo resolver (stub for MVP).
	resolver := geo.NewStubResolver()

	// Build worker.
	worker = NewWorker(dynamoStore, resolver, clicksTTLDays*24*time.Hour)
}

func main() {
	if os.Getenv("LOCAL_MODE") == "true" {
		fmt.Println("[worker] LOCAL_MODE enabled, starting HTTP server on :8082")
		http.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read body", http.StatusBadRequest)
				return
			}
			defer r.Body.Close()

			var event events.SQSEvent
			if err := json.Unmarshal(body, &event); err != nil {
				http.Error(w, "invalid SQS event JSON: "+err.Error(), http.StatusBadRequest)
				return
			}

			resp, err := worker.HandleSQSEvent(r.Context(), event)
			if err != nil {
				http.Error(w, "handler error: "+err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		if err := http.ListenAndServe(":8082", nil); err != nil {
			panic(err)
		}
		return
	}

	lambda.Start(worker.HandleSQSEvent)
}

// hashString computes SHA-256 of a string and returns its hex encoding.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// envOrDefault reads an env var with a fallback default value.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

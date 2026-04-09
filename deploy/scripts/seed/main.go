// Package main seeds DynamoDB with sample data for local development.
// It inserts a test user, several example short links, and a few click events.
//
// Prerequisites: tables must exist (run migrate first).
//
// Usage:
//
//	go run ./deploy/scripts/seed
//	AWS_ENDPOINT_URL=http://localhost:4566 go run ./deploy/scripts/seed
package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/romanitalian/shorty/internal/store"
)

const (
	linksTable  = "shorty-links"
	clicksTable = "shorty-clicks"
	usersTable  = "shorty-users"

	// Fixed test user Cognito sub for local dev.
	testUserSub = "00000000-0000-0000-0000-000000000001"
	ipHashSalt  = "local-dev-salt-change-in-prod"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := newDynamoDBClient(ctx)
	if err != nil {
		log.Fatalf("failed to create DynamoDB client: %v", err)
	}

	now := time.Now().Unix()
	today := time.Now().Format("2006-01-02")

	// -------------------------------------------------------------------------
	// Seed: test user
	// -------------------------------------------------------------------------
	testUser := store.User{
		PK:                fmt.Sprintf("USER#%s", testUserSub),
		SK:                "PROFILE",
		Email:             "dev@shorty.local",
		DisplayName:       "Local Dev User",
		Plan:              "free",
		DailyLinkQuota:    50,
		TotalLinkQuota:    500,
		LinksCreatedToday: 0,
		TotalActiveLinks:  0,
		LastResetDate:     today,
		CreatedAt:         now - 86400*30, // created 30 days ago
	}
	putItem(ctx, client, usersTable, testUser, "test user")

	// -------------------------------------------------------------------------
	// Seed: example links
	// -------------------------------------------------------------------------
	links := []store.Link{
		{
			PK:          "LINK#abc123",
			SK:          "META",
			OwnerID:     testUserSub,
			OriginalURL: "https://github.com/romanitalian/shorty",
			Code:        "abc123",
			Title:       "Shorty GitHub Repo",
			IsActive:    true,
			ClickCount:  42,
			UTMSource:   "twitter",
			UTMMedium:   "social",
			UTMCampaign: "launch",
			CreatedAt:   now - 86400*7, // created 7 days ago
			UpdatedAt:   now - 86400*1,
		},
		{
			PK:          "LINK#golang",
			SK:          "META",
			OwnerID:     testUserSub,
			OriginalURL: "https://go.dev/doc/",
			Code:        "golang",
			Title:       "Go Documentation",
			IsActive:    true,
			ClickCount:  128,
			CreatedAt:   now - 86400*14,
			UpdatedAt:   now - 86400*2,
		},
		{
			PK:          "LINK#expired1",
			SK:          "META",
			OwnerID:     testUserSub,
			OriginalURL: "https://example.com/old-promo",
			Code:        "expired1",
			Title:       "Expired Promo Link",
			ExpiresAt:   int64Ptr(now - 3600), // expired 1 hour ago
			IsActive:    true,
			ClickCount:  5,
			CreatedAt:   now - 86400*30,
			UpdatedAt:   now - 86400*30,
		},
		{
			PK:          "LINK#limited",
			SK:          "META",
			OwnerID:     testUserSub,
			OriginalURL: "https://example.com/limited-offer",
			Code:        "limited",
			Title:       "Click-Limited Link",
			MaxClicks:   int64Ptr(100),
			IsActive:    true,
			ClickCount:  97,
			CreatedAt:   now - 86400*3,
			UpdatedAt:   now - 86400*1,
		},
		{
			PK:          "LINK#anon01",
			SK:          "META",
			OwnerID:     "anonymous",
			OriginalURL: "https://example.com/public",
			Code:        "anon01",
			Title:       "",
			IsActive:    true,
			ClickCount:  3,
			ExpiresAt:   int64Ptr(now + 86400*7), // expires in 7 days
			CreatedAt:   now - 86400*1,
			UpdatedAt:   now - 86400*1,
		},
	}

	for _, link := range links {
		putItem(ctx, client, linksTable, link, fmt.Sprintf("link %s", link.Code))
	}

	// Update user active link count to match seeded links.
	testUser.TotalActiveLinks = int64(len(links) - 1) // exclude "anon01"
	testUser.LinksCreatedToday = 0
	putItem(ctx, client, usersTable, testUser, "test user (updated counts)")

	// -------------------------------------------------------------------------
	// Seed: click events for "abc123"
	// -------------------------------------------------------------------------
	clickSamples := []struct {
		code      string
		ip        string
		country   string
		device    string
		referer   string
		userAgent string
		ageHours  int64
	}{
		{"abc123", "192.168.1.1", "US", "desktop", "twitter.com", "Mozilla/5.0", 2},
		{"abc123", "10.0.0.1", "DE", "mobile", "t.co", "Safari/16", 5},
		{"abc123", "172.16.0.1", "JP", "tablet", "", "Chrome/120", 12},
		{"abc123", "192.168.1.1", "US", "desktop", "reddit.com", "Mozilla/5.0", 24},
		{"abc123", "10.0.0.2", "GB", "mobile", "linkedin.com", "Safari/17", 48},
		{"golang", "192.168.1.1", "US", "desktop", "google.com", "Chrome/120", 3},
		{"golang", "10.0.0.5", "FR", "mobile", "", "Firefox/121", 6},
	}

	for i, c := range clickSamples {
		ts := now - c.ageHours*3600
		click := store.ClickEvent{
			PK:            fmt.Sprintf("LINK#%s", c.code),
			SK:            fmt.Sprintf("CLICK#%d#seed-%04d", ts, i),
			IPHash:        hashIP(c.ip),
			Country:       c.country,
			DeviceType:    c.device,
			RefererDomain: c.referer,
			UserAgentHash: hashString(c.userAgent),
			CreatedAt:     ts,
			TTL:           ts + 90*86400, // 90-day retention
		}
		putItem(ctx, client, clicksTable, click, fmt.Sprintf("click %s #%d", c.code, i))
	}

	fmt.Println()
	log.Printf("seed complete: 1 user, %d links, %d click events", len(links), len(clickSamples))
}

// newDynamoDBClient creates an AWS SDK DynamoDB client.
func newDynamoDBClient(ctx context.Context) (*dynamodb.Client, error) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	opts := []func(*dynamodb.Options){}
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		opts = append(opts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	return dynamodb.NewFromConfig(cfg, opts...), nil
}

// putItem marshals the item and puts it into the specified table.
func putItem(ctx context.Context, client *dynamodb.Client, table string, item any, label string) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		log.Fatalf("failed to marshal %s: %v", label, err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(table),
		Item:      av,
	})
	if err != nil {
		log.Fatalf("failed to put %s into %s: %v", label, table, err)
	}
	log.Printf("  seeded: %s", label)
}

// hashIP computes SHA-256(ip + salt) and returns a hex string.
// Matches the production IP hashing scheme.
func hashIP(ip string) string {
	return hashString(ip + ipHashSalt)
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

func int64Ptr(v int64) *int64 {
	return &v
}

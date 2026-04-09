// Package main provides a DynamoDB table migration script for the Shorty URL shortener.
// It creates all three tables (links, clicks, users) with proper key schemas, GSIs,
// and TTL settings. Designed to run against LocalStack for local dev or real AWS.
//
// Usage:
//
//	go run ./deploy/scripts/migrate
//	AWS_ENDPOINT_URL=http://localhost:4566 go run ./deploy/scripts/migrate
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	linksTable  = "shorty-links"
	clicksTable = "shorty-clicks"
	usersTable  = "shorty-users"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := newDynamoDBClient(ctx)
	if err != nil {
		log.Fatalf("failed to create DynamoDB client: %v", err)
	}

	tables := []struct {
		name   string
		create func(context.Context, *dynamodb.Client) error
		ttl    *types.TimeToLiveSpecification
	}{
		{
			name:   linksTable,
			create: createLinksTable,
			ttl: &types.TimeToLiveSpecification{
				AttributeName: aws.String("expires_at"),
				Enabled:       aws.Bool(true),
			},
		},
		{
			name:   clicksTable,
			create: createClicksTable,
			ttl: &types.TimeToLiveSpecification{
				AttributeName: aws.String("ttl"),
				Enabled:       aws.Bool(true),
			},
		},
		{
			name:   usersTable,
			create: createUsersTable,
			ttl:    nil, // no TTL for users
		},
	}

	for _, t := range tables {
		if tableExists(ctx, client, t.name) {
			log.Printf("table %s already exists, skipping creation", t.name)
		} else {
			log.Printf("creating table %s ...", t.name)
			if err := t.create(ctx, client); err != nil {
				log.Fatalf("failed to create table %s: %v", t.name, err)
			}
			if err := waitForTable(ctx, client, t.name); err != nil {
				log.Fatalf("table %s did not become active: %v", t.name, err)
			}
			log.Printf("table %s created successfully", t.name)
		}

		// Enable TTL (idempotent -- DynamoDB ignores if already enabled).
		if t.ttl != nil {
			log.Printf("enabling TTL on %s (attribute: %s)", t.name, *t.ttl.AttributeName)
			_, err := client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
				TableName:               aws.String(t.name),
				TimeToLiveSpecification: t.ttl,
			})
			if err != nil {
				// LocalStack and DynamoDB return an error if TTL is already enabled.
				// Treat ValidationException as a non-fatal case.
				var ve *types.ResourceInUseException
				if !errors.As(err, &ve) {
					log.Printf("warning: TTL update for %s: %v (may already be enabled)", t.name, err)
				}
			}
		}
	}

	fmt.Println()
	log.Println("migration complete -- all tables ready")
}

// newDynamoDBClient creates an AWS SDK DynamoDB client.
// If AWS_ENDPOINT_URL is set, it overrides the endpoint (for LocalStack).
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

func tableExists(ctx context.Context, client *dynamodb.Client, name string) bool {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	return err == nil
}

func waitForTable(ctx context.Context, client *dynamodb.Client, name string) error {
	for range 30 {
		out, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(name),
		})
		if err == nil && out.Table.TableStatus == types.TableStatusActive {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timeout waiting for table %s to become active", name)
}

// createLinksTable creates the shorty-links table with the owner_id-created_at GSI.
// Key schema: PK (S) HASH, SK (S) RANGE
// GSI: owner_id-created_at-index (owner_id HASH, created_at RANGE, projection ALL)
// TTL: expires_at
func createLinksTable(ctx context.Context, client *dynamodb.Client) error {
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(linksTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("owner_id"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("created_at"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("owner_id-created_at-index"),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("owner_id"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("created_at"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{
					ProjectionType: types.ProjectionTypeAll,
				},
			},
		},
		BillingMode: types.BillingModePayPerRequest,
		Tags: []types.Tag{
			{Key: aws.String("Project"), Value: aws.String("shorty")},
			{Key: aws.String("Table"), Value: aws.String("links")},
			{Key: aws.String("ManagedBy"), Value: aws.String("terraform")},
		},
	})
	return err
}

// createClicksTable creates the shorty-clicks table with the code-date GSI.
// Key schema: PK (S) HASH, SK (S) RANGE
// GSI: code-date-index (PK HASH, created_at RANGE, projection INCLUDE)
// TTL: ttl (dedicated attribute, not created_at)
func createClicksTable(ctx context.Context, client *dynamodb.Client) error {
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(clicksTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("created_at"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String("code-date-index"),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("created_at"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{
					ProjectionType:   types.ProjectionTypeInclude,
					NonKeyAttributes: []string{"country", "device_type", "referer_domain", "ip_hash"},
				},
			},
		},
		BillingMode: types.BillingModePayPerRequest, // PAY_PER_REQUEST for local dev; prod uses provisioned via Terraform
		Tags: []types.Tag{
			{Key: aws.String("Project"), Value: aws.String("shorty")},
			{Key: aws.String("Table"), Value: aws.String("clicks")},
			{Key: aws.String("ManagedBy"), Value: aws.String("terraform")},
		},
	})
	return err
}

// createUsersTable creates the shorty-users table (no GSI, no TTL).
// Key schema: PK (S) HASH, SK (S) RANGE
func createUsersTable(ctx context.Context, client *dynamodb.Client) error {
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(usersTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange},
		},
		BillingMode: types.BillingModePayPerRequest,
		Tags: []types.Tag{
			{Key: aws.String("Project"), Value: aws.String("shorty")},
			{Key: aws.String("Table"), Value: aws.String("users")},
			{Key: aws.String("ManagedBy"), Value: aws.String("terraform")},
		},
	})
	return err
}

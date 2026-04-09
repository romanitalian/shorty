package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestInit_EmptyEndpoint_ReturnsNoOp(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, Config{
		ServiceName:    "test-service",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		OTLPEndpoint:   "", // empty -> no-op
	})
	if err != nil {
		t.Fatalf("Init with empty endpoint should not error, got: %v", err)
	}

	// Verify a tracer is available (no panic) and produces no-op spans.
	tp := otel.GetTracerProvider()
	tracer := tp.Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	// A no-op span should not be recording.
	if span.IsRecording() {
		t.Error("expected no-op span to not be recording")
	}
	span.End()

	// Shutdown must be callable without error.
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown should not error, got: %v", err)
	}
}

func TestInit_ShutdownCallable(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, Config{
		ServiceName:  "test-svc",
		OTLPEndpoint: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Calling shutdown multiple times should be safe.
	if err := shutdown(ctx); err != nil {
		t.Errorf("first shutdown call failed: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Errorf("second shutdown call failed: %v", err)
	}
}

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/store"
)

// ---------- Stats integration tests ----------

func TestGetLinkStats_ValidOwner(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["st1"] = &store.Link{
		Code:        "st1",
		OriginalURL: "https://example.com",
		OwnerID:     "owner1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ms.statsMap["st1"] = &store.LinkStats{
		TotalClicks:  150,
		UniqueClicks: 80,
	}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/st1/stats", nil, authHeaders("owner1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsAggregate
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalClicks != 150 {
		t.Fatalf("expected total_clicks 150, got %d", resp.TotalClicks)
	}
	if resp.UniqueClicks != 80 {
		t.Fatalf("expected unique_clicks 80, got %d", resp.UniqueClicks)
	}
	if resp.Code != "st1" {
		t.Fatalf("expected code st1, got %s", resp.Code)
	}
}

func TestGetLinkStats_WrongOwner(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["st2"] = &store.Link{
		Code:        "st2",
		OriginalURL: "https://example.com",
		OwnerID:     "owner1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ms.statsMap["st2"] = &store.LinkStats{TotalClicks: 10}

	// Request as a different user ("attacker") -- should get 404 (not 403)
	// to prevent link enumeration.
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/st2/stats", nil, authHeaders("attacker"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong owner, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetLinkStats_Unauthenticated(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["st3"] = &store.Link{
		Code:        "st3",
		OriginalURL: "https://example.com",
		OwnerID:     "owner1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// No auth header -- getUserID falls back to "anonymous".
	// Link is owned by "owner1", so ownership check fails => 404.
	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/st3/stats", nil, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unauthenticated user, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetLinkTimeline_HourGranularity(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["tl1"] = &store.Link{
		Code:        "tl1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	h1 := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC).Unix()
	h2 := time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC).Unix()
	h3 := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC).Unix()
	ms.timelineMap["tl1"] = []store.TimelineBucket{
		{Timestamp: h1, Clicks: 5},
		{Timestamp: h2, Clicks: 12},
		{Timestamp: h3, Clicks: 3},
	}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/tl1/stats/timeline?granularity=hour", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsTimeline
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Granularity != "hour" {
		t.Fatalf("expected granularity hour, got %s", resp.Granularity)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(resp.Data))
	}
	for _, dp := range resp.Data {
		if _, err := time.Parse(time.RFC3339, dp.Date); err != nil {
			t.Fatalf("expected RFC3339 date format for hour granularity, got %q: %v", dp.Date, err)
		}
	}
}

func TestGetLinkTimeline_DayGranularity(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["tl2"] = &store.Link{
		Code:        "tl2",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	d1 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	d2 := time.Date(2024, 3, 2, 0, 0, 0, 0, time.UTC).Unix()
	ms.timelineMap["tl2"] = []store.TimelineBucket{
		{Timestamp: d1, Clicks: 20},
		{Timestamp: d2, Clicks: 35},
	}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/tl2/stats/timeline?granularity=day", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsTimeline
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Granularity != "day" {
		t.Fatalf("expected granularity day, got %s", resp.Granularity)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(resp.Data))
	}
	for _, dp := range resp.Data {
		if _, err := time.Parse("2006-01-02", dp.Date); err != nil {
			t.Fatalf("expected YYYY-MM-DD date format for day granularity, got %q: %v", dp.Date, err)
		}
	}
}

func TestGetLinkGeo_MultipleCountries(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["geo1"] = &store.Link{
		Code:        "geo1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ms.geoMap["geo1"] = []store.GeoStat{
		{Country: "US", Clicks: 50},
		{Country: "DE", Clicks: 30},
		{Country: "JP", Clicks: 20},
	}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/geo1/stats/geo", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsGeo
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 countries, got %d", len(resp.Data))
	}
	if resp.Data[0].Country != "US" {
		t.Fatalf("expected first country US, got %s", resp.Data[0].Country)
	}
	if resp.Data[0].Clicks != 50 {
		t.Fatalf("expected first clicks 50, got %d", resp.Data[0].Clicks)
	}
}

func TestGetLinkReferrers_MultipleReferrers(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["ref1"] = &store.Link{
		Code:        "ref1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ms.referrerMap["ref1"] = []store.ReferrerStat{
		{Domain: "twitter.com", Clicks: 40},
		{Domain: "reddit.com", Clicks: 25},
		{Domain: "direct", Clicks: 15},
	}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/ref1/stats/referrers", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsReferrers
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 referrers, got %d", len(resp.Data))
	}
	if resp.Data[0].Domain != "twitter.com" {
		t.Fatalf("expected first referrer twitter.com, got %s", resp.Data[0].Domain)
	}
	if resp.Data[0].Clicks != 40 {
		t.Fatalf("expected first clicks 40, got %d", resp.Data[0].Clicks)
	}

	found := false
	for _, r := range resp.Data {
		if r.Domain == "direct" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'direct' referrer entry")
	}
}

// ---------- Stats edge cases ----------

func TestGetLinkStats_NonexistentCode(t *testing.T) {
	_, handler, _ := setupTestServer(t)

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/nosuchcode/stats", nil, authHeaders("user1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetLinkStats_ZeroClicks(t *testing.T) {
	_, handler, ms := setupTestServer(t)

	now := time.Now().Unix()
	ms.links["empty1"] = &store.Link{
		Code:        "empty1",
		OriginalURL: "https://example.com",
		OwnerID:     "user1",
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ms.statsMap["empty1"] = &store.LinkStats{TotalClicks: 0, UniqueClicks: 0}

	rec := doJSONRequest(handler, http.MethodGet, "/api/v1/links/empty1/stats", nil, authHeaders("user1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp gen.StatsAggregate
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalClicks != 0 {
		t.Fatalf("expected total_clicks 0, got %d", resp.TotalClicks)
	}
	if resp.UniqueClicks != 0 {
		t.Fatalf("expected unique_clicks 0, got %d", resp.UniqueClicks)
	}
}

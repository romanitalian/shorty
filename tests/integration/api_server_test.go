package integration

// This file provides a minimal API server implementation for integration testing.
// It mirrors the behavior of cmd/api/server.go but lives in a test package so
// it can be composed with mock dependencies. Only the handlers needed for stats,
// auth, and security tests are fully implemented.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	openapi_types "github.com/oapi-codegen/runtime/types"
	gen "github.com/romanitalian/shorty/internal/api/generated"
	"github.com/romanitalian/shorty/internal/auth"
	"github.com/romanitalian/shorty/internal/cache"
	"github.com/romanitalian/shorty/internal/ratelimit"
	"github.com/romanitalian/shorty/internal/shortener"
	"github.com/romanitalian/shorty/internal/store"
	"github.com/romanitalian/shorty/internal/validator"
	"github.com/romanitalian/shorty/pkg/apierr"
)

const (
	guestCreateLimit  int64 = 5
	guestCreateWindow       = 1 * time.Hour
	userCreateLimit   int64 = 50
	userCreateWindow        = 1 * time.Hour
	maxGuestTTL             = 24 * time.Hour
	headerUserID            = "X-User-Id"
)

func testBaseURL() string {
	if v := os.Getenv("SHORT_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

// integrationAPIServer implements gen.ServerInterface for integration testing.
type integrationAPIServer struct {
	gen.Unimplemented
	store     store.Store
	cache     cache.Cache
	limiter   ratelimit.Limiter
	generator shortener.Generator
	validator validator.Validator
}

func newIntegrationAPIServer(
	s store.Store,
	c cache.Cache,
	l ratelimit.Limiter,
	g shortener.Generator,
	v validator.Validator,
) *integrationAPIServer {
	return &integrationAPIServer{
		store:     s,
		cache:     c,
		limiter:   l,
		generator: g,
		validator: v,
	}
}

func getUserID(r *http.Request) string {
	if uid := auth.UserIDFromContext(r.Context()); uid != "" {
		return uid
	}
	if uid := r.Header.Get(headerUserID); uid != "" {
		return uid
	}
	return "anonymous"
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func setRateLimitHeaders(w http.ResponseWriter, res *ratelimit.Result) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(res.Limit, 10))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(res.ResetAt.Unix(), 10))
	if !res.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())+1))
	}
}

func storeLinkToGenLink(l *store.Link) gen.Link {
	link := gen.Link{
		Code:        l.Code,
		OriginalUrl: l.OriginalURL,
		ShortUrl:    testBaseURL() + "/" + l.Code,
		OwnerId:     l.OwnerID,
		ClickCount:  int(l.ClickCount),
		IsActive:    l.IsActive,
		HasPassword: l.PasswordHash != "",
		CreatedAt:   time.Unix(l.CreatedAt, 0).UTC(),
		UpdatedAt:   time.Unix(l.UpdatedAt, 0).UTC(),
	}
	if l.Title != "" {
		link.Title = &l.Title
	}
	if l.ExpiresAt != nil {
		t := time.Unix(*l.ExpiresAt, 0).UTC()
		link.ExpiresAt = &t
	}
	if l.MaxClicks != nil {
		mc := int(*l.MaxClicks)
		link.MaxClicks = &mc
	}
	return link
}

func defaultStatsPeriod() (time.Time, time.Time) {
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24*time.Hour - time.Second)
	from := to.Add(-30 * 24 * time.Hour)
	return from, to
}

func statsPeriodFromParams(fromParam *openapi_types.Date, toParam *openapi_types.Date) (time.Time, time.Time, gen.StatsPeriod) {
	from, to := defaultStatsPeriod()
	if fromParam != nil {
		from = fromParam.Time
	}
	if toParam != nil {
		to = toParam.Time.Add(24*time.Hour - time.Second)
	}
	period := gen.StatsPeriod{
		From: openapi_types.Date{Time: from.Truncate(24 * time.Hour)},
		To:   openapi_types.Date{Time: to.Truncate(24 * time.Hour)},
	}
	return from, to, period
}

func (s *integrationAPIServer) verifyLinkOwnership(ctx context.Context, code, userID string) (*store.Link, error) {
	link, err := s.store.GetLink(ctx, code)
	if err != nil {
		return nil, err
	}
	if link.OwnerID != userID {
		return nil, store.ErrLinkNotFound
	}
	return link, nil
}

// ---------- Guest Shorten ----------

func (s *integrationAPIServer) GuestShorten(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rlKey := fmt.Sprintf("rl:create:guest:%s", clientIP(r))
	rlRes, err := s.limiter.Allow(ctx, rlKey, guestCreateLimit, guestCreateWindow)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("rate limiter error"))
		return
	}
	setRateLimitHeaders(w, rlRes)
	if !rlRes.Allowed {
		apierr.WriteProblem(w, apierr.TooManyRequests("rate limit exceeded"))
		return
	}

	var req gen.GuestShortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteProblem(w, apierr.BadRequest("invalid request body"))
		return
	}

	if err := s.validator.ValidateURL(ctx, req.Url); err != nil {
		apierr.WriteProblem(w, apierr.BadRequest(err.Error()))
		return
	}

	code, err := s.generator.Generate(ctx)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("code generation failed"))
		return
	}

	now := time.Now()
	expiresAt := now.Add(maxGuestTTL).Unix()
	link := &store.Link{
		Code:        code,
		OriginalURL: req.Url,
		OwnerID:     "anonymous",
		IsActive:    true,
		ExpiresAt:   &expiresAt,
		CreatedAt:   now.Unix(),
		UpdatedAt:   now.Unix(),
	}

	if err := s.store.CreateLink(ctx, link); err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to create link"))
		return
	}

	_ = s.cache.SetLink(ctx, code, link, 0)

	resp := gen.GuestShortenResponse{
		Code:        code,
		OriginalUrl: req.Url,
		ShortUrl:    testBaseURL() + "/" + code,
		ExpiresAt:   time.Unix(expiresAt, 0).UTC(),
	}
	apierr.WriteJSON(w, http.StatusCreated, resp)
}

// ---------- Create Link ----------

func (s *integrationAPIServer) CreateLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := getUserID(r)

	rlKey := fmt.Sprintf("rl:create:user:%s", userID)
	rlRes, err := s.limiter.Allow(ctx, rlKey, userCreateLimit, userCreateWindow)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("rate limiter error"))
		return
	}
	setRateLimitHeaders(w, rlRes)
	if !rlRes.Allowed {
		apierr.WriteProblem(w, apierr.TooManyRequests("rate limit exceeded"))
		return
	}

	var req gen.CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteProblem(w, apierr.BadRequest("invalid request body"))
		return
	}

	if err := s.validator.ValidateURL(ctx, req.Url); err != nil {
		apierr.WriteProblem(w, apierr.BadRequest(err.Error()))
		return
	}

	var code string
	if req.CustomCode != nil && *req.CustomCode != "" {
		if err := s.generator.GenerateCustom(ctx, *req.CustomCode); err != nil {
			if errors.Is(err, store.ErrCodeCollision) {
				apierr.WriteProblem(w, apierr.Conflict("custom alias already in use"))
				return
			}
			apierr.WriteProblem(w, apierr.InternalError("code validation failed"))
			return
		}
		code = *req.CustomCode
	} else {
		generated, err := s.generator.Generate(ctx)
		if err != nil {
			apierr.WriteProblem(w, apierr.InternalError("code generation failed"))
			return
		}
		code = generated
	}

	now := time.Now()
	link := &store.Link{
		Code:        code,
		OriginalURL: req.Url,
		OwnerID:     userID,
		IsActive:    true,
		CreatedAt:   now.Unix(),
		UpdatedAt:   now.Unix(),
	}
	if req.Title != nil {
		link.Title = *req.Title
	}
	if req.ExpiresAt != nil {
		exp := req.ExpiresAt.Unix()
		link.ExpiresAt = &exp
	}
	if req.MaxClicks != nil {
		mc := int64(*req.MaxClicks)
		link.MaxClicks = &mc
	}
	if req.Password != nil && *req.Password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			apierr.WriteProblem(w, apierr.InternalError("failed to hash password"))
			return
		}
		link.PasswordHash = string(hash)
	}

	if err := s.store.CreateLink(ctx, link); err != nil {
		if errors.Is(err, store.ErrCodeCollision) {
			apierr.WriteProblem(w, apierr.Conflict("short code collision"))
			return
		}
		apierr.WriteProblem(w, apierr.InternalError("failed to create link"))
		return
	}

	_ = s.cache.SetLink(ctx, code, link, 0)
	apierr.WriteJSON(w, http.StatusCreated, storeLinkToGenLink(link))
}

// ---------- List / Get / Update / Delete ----------

func (s *integrationAPIServer) ListLinks(w http.ResponseWriter, r *http.Request, params gen.ListLinksParams) {
	ctx := r.Context()
	userID := getUserID(r)

	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	limit := 20
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
	}

	links, nextCursor, err := s.store.ListLinksByOwner(ctx, userID, cursor, limit)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to list links"))
		return
	}

	items := make([]gen.Link, 0, len(links))
	for _, l := range links {
		items = append(items, storeLinkToGenLink(l))
	}

	resp := gen.LinkListResponse{
		Items: items,
		Pagination: gen.Pagination{
			HasMore: nextCursor != "",
		},
	}
	if nextCursor != "" {
		resp.Pagination.NextCursor = &nextCursor
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

func (s *integrationAPIServer) GetLink(w http.ResponseWriter, r *http.Request, code gen.CodePath) {
	ctx := r.Context()
	userID := getUserID(r)

	link, err := s.store.GetLink(ctx, code)
	if err != nil {
		if errors.Is(err, store.ErrLinkNotFound) {
			apierr.WriteProblem(w, apierr.NotFound("link not found"))
			return
		}
		apierr.WriteProblem(w, apierr.InternalError("failed to get link"))
		return
	}
	if link.OwnerID != userID {
		apierr.WriteProblem(w, apierr.NotFound("link not found"))
		return
	}
	apierr.WriteJSON(w, http.StatusOK, storeLinkToGenLink(link))
}

func (s *integrationAPIServer) UpdateLink(w http.ResponseWriter, r *http.Request, code gen.CodePath) {
	ctx := r.Context()
	userID := getUserID(r)

	var req gen.UpdateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteProblem(w, apierr.BadRequest("invalid request body"))
		return
	}

	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if err := s.store.UpdateLink(ctx, code, userID, updates); err != nil {
		if errors.Is(err, store.ErrLinkNotFound) {
			apierr.WriteProblem(w, apierr.NotFound("link not found"))
			return
		}
		apierr.WriteProblem(w, apierr.InternalError("failed to update link"))
		return
	}

	_ = s.cache.DeleteLink(ctx, code)

	link, err := s.store.GetLink(ctx, code)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to fetch updated link"))
		return
	}
	apierr.WriteJSON(w, http.StatusOK, storeLinkToGenLink(link))
}

func (s *integrationAPIServer) DeleteLink(w http.ResponseWriter, r *http.Request, code gen.CodePath) {
	ctx := r.Context()
	userID := getUserID(r)

	if err := s.store.DeleteLink(ctx, code, userID); err != nil {
		if errors.Is(err, store.ErrLinkNotFound) {
			apierr.WriteProblem(w, apierr.NotFound("link not found"))
			return
		}
		apierr.WriteProblem(w, apierr.InternalError("failed to delete link"))
		return
	}
	_ = s.cache.DeleteLink(ctx, code)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Profile ----------

func (s *integrationAPIServer) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	email := "user@example.com"

	if claims, ok := auth.ClaimsFromContext(r.Context()); ok {
		if claims.Email != "" {
			email = claims.Email
		}
	}

	resp := gen.UserProfile{
		UserId:    userID,
		Email:     openapi_types.Email(email),
		Plan:      gen.Free,
		CreatedAt: time.Now().UTC(),
		Quota: gen.QuotaUsage{
			DailyLinksUsed:   0,
			DailyLinksLimit:  50,
			TotalLinksActive: 0,
			TotalLinksLimit:  500,
		},
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

// ---------- Stats ----------

func (s *integrationAPIServer) GetLinkStats(w http.ResponseWriter, r *http.Request, code gen.CodePath, params gen.GetLinkStatsParams) {
	ctx := r.Context()
	userID := getUserID(r)

	if _, err := s.verifyLinkOwnership(ctx, code, userID); err != nil {
		apierr.WriteProblem(w, apierr.NotFound("link not found"))
		return
	}

	stats, err := s.store.GetLinkStats(ctx, code)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to get stats"))
		return
	}

	_, _, period := statsPeriodFromParams(params.From, params.To)

	resp := gen.StatsAggregate{
		Code:         code,
		TotalClicks:  int(stats.TotalClicks),
		UniqueClicks: int(stats.UniqueClicks),
		Period:       period,
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

func (s *integrationAPIServer) GetLinkStatsGeo(w http.ResponseWriter, r *http.Request, code gen.CodePath, params gen.GetLinkStatsGeoParams) {
	ctx := r.Context()
	userID := getUserID(r)

	if _, err := s.verifyLinkOwnership(ctx, code, userID); err != nil {
		apierr.WriteProblem(w, apierr.NotFound("link not found"))
		return
	}

	geoStats, err := s.store.GetLinkGeo(ctx, code)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to get geo stats"))
		return
	}

	_, _, period := statsPeriodFromParams(params.From, params.To)

	data := make([]gen.GeoDataPoint, 0, len(geoStats))
	for _, g := range geoStats {
		data = append(data, gen.GeoDataPoint{
			Country: g.Country,
			Clicks:  int(g.Clicks),
		})
	}

	resp := gen.StatsGeo{
		Code:   code,
		Data:   data,
		Period: period,
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

func (s *integrationAPIServer) GetLinkStatsReferrers(w http.ResponseWriter, r *http.Request, code gen.CodePath, params gen.GetLinkStatsReferrersParams) {
	ctx := r.Context()
	userID := getUserID(r)

	if _, err := s.verifyLinkOwnership(ctx, code, userID); err != nil {
		apierr.WriteProblem(w, apierr.NotFound("link not found"))
		return
	}

	refStats, err := s.store.GetLinkReferrers(ctx, code)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to get referrer stats"))
		return
	}

	_, _, period := statsPeriodFromParams(params.From, params.To)

	data := make([]gen.ReferrerDataPoint, 0, len(refStats))
	for _, r := range refStats {
		data = append(data, gen.ReferrerDataPoint{
			Domain: r.Domain,
			Clicks: int(r.Clicks),
		})
	}

	resp := gen.StatsReferrers{
		Code:   code,
		Data:   data,
		Period: period,
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

func (s *integrationAPIServer) GetLinkStatsTimeline(w http.ResponseWriter, r *http.Request, code gen.CodePath, params gen.GetLinkStatsTimelineParams) {
	ctx := r.Context()
	userID := getUserID(r)

	if _, err := s.verifyLinkOwnership(ctx, code, userID); err != nil {
		apierr.WriteProblem(w, apierr.NotFound("link not found"))
		return
	}

	granularity := "day"
	if params.Granularity != nil {
		granularity = string(*params.Granularity)
	}

	from, to, period := statsPeriodFromParams(params.From, params.To)

	buckets, err := s.store.GetLinkTimeline(ctx, code, from, to, granularity)
	if err != nil {
		apierr.WriteProblem(w, apierr.InternalError("failed to get timeline stats"))
		return
	}

	data := make([]gen.TimelineDataPoint, 0, len(buckets))
	for _, b := range buckets {
		t := time.Unix(b.Timestamp, 0).UTC()
		var dateStr string
		if granularity == "hour" {
			dateStr = t.Format(time.RFC3339)
		} else {
			dateStr = t.Format("2006-01-02")
		}
		data = append(data, gen.TimelineDataPoint{
			Date:         dateStr,
			Clicks:       int(b.Clicks),
			UniqueClicks: 0,
		})
	}

	resp := gen.StatsTimeline{
		Code:        code,
		Data:        data,
		Granularity: gen.StatsTimelineGranularity(granularity),
		Period:      period,
	}
	apierr.WriteJSON(w, http.StatusOK, resp)
}

// ---------- Redirect stubs ----------

func (s *integrationAPIServer) RedirectToOriginal(w http.ResponseWriter, r *http.Request, code gen.CodePath) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *integrationAPIServer) SubmitPassword(w http.ResponseWriter, r *http.Request, code gen.CodePath) {
	w.WriteHeader(http.StatusNotImplemented)
}

// Compile-time check.
var _ gen.ServerInterface = (*integrationAPIServer)(nil)

// Suppress unused import warnings.
var _ = context.Background

package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/romanitalian/shorty/pkg/apierr"
)

// Middleware returns an HTTP middleware that extracts and validates a JWT token
// from the Authorization header (Bearer scheme) or the "session" cookie.
//
// If no token is present, the request proceeds as anonymous (no claims in context).
// If a token is present but invalid, the middleware responds with 401.
// If a token is valid, Claims are placed in the request context and the
// X-User-Id header is set to the subject (Cognito sub).
func Middleware(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := extractToken(r)

			// No token: anonymous request.
			if tokenString == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := auth.ValidateToken(r.Context(), tokenString)
			if err != nil {
				apierr.WriteProblem(w, apierr.Unauthorized("invalid or expired token"))
				return
			}

			// Store claims in context and set X-User-Id header.
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			r.Header.Set("X-User-Id", claims.Subject)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth is a middleware that returns 401 if the request has no valid claims
// in context. It should be applied after Middleware on routes that require
// authentication.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := ClaimsFromContext(r.Context()); !ok {
			apierr.WriteProblem(w, apierr.Unauthorized("authentication required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFromContext retrieves the Claims stored by the auth middleware.
// Returns nil, false if no claims are present (anonymous request).
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// UserIDFromContext returns the user's subject identifier from the context.
// Returns an empty string for anonymous requests.
func UserIDFromContext(ctx context.Context) string {
	c, ok := ClaimsFromContext(ctx)
	if !ok || c == nil {
		return ""
	}
	return c.Subject
}

// extractToken looks for a JWT first in the Authorization header (Bearer scheme)
// and then in the "session" cookie.
func extractToken(r *http.Request) string {
	// Check Authorization header.
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(authHeader, prefix) {
			return strings.TrimPrefix(authHeader, prefix)
		}
	}

	// Check session cookie.
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	return ""
}

// Package apierr provides standardized API error types following RFC 7807 Problem Details.
package apierr

import (
	"encoding/json"
	"net/http"
)

// ProblemDetail implements RFC 7807 error responses.
type ProblemDetail struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// FieldError represents a validation error on a specific field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (p *ProblemDetail) Error() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// JSON returns the JSON representation of the problem detail.
func (p *ProblemDetail) JSON() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// BadRequest creates a 400 Bad Request problem detail.
func BadRequest(detail string, fieldErrors ...FieldError) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Bad Request",
		Status: http.StatusBadRequest,
		Detail: detail,
		Errors: fieldErrors,
	}
}

// NotFound creates a 404 Not Found problem detail.
func NotFound(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Not Found",
		Status: http.StatusNotFound,
		Detail: detail,
	}
}

// Unauthorized creates a 401 Unauthorized problem detail.
func Unauthorized(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Unauthorized",
		Status: http.StatusUnauthorized,
		Detail: detail,
	}
}

// Forbidden creates a 403 Forbidden problem detail.
func Forbidden(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Forbidden",
		Status: http.StatusForbidden,
		Detail: detail,
	}
}

// Conflict creates a 409 Conflict problem detail.
func Conflict(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Conflict",
		Status: http.StatusConflict,
		Detail: detail,
	}
}

// Gone creates a 410 Gone problem detail.
func Gone(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Gone",
		Status: http.StatusGone,
		Detail: detail,
	}
}

// TooManyRequests creates a 429 Too Many Requests problem detail.
func TooManyRequests(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Too Many Requests",
		Status: http.StatusTooManyRequests,
		Detail: detail,
	}
}

// UnprocessableEntity creates a 422 Unprocessable Entity problem detail.
func UnprocessableEntity(detail string, fieldErrors ...FieldError) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Unprocessable Entity",
		Status: http.StatusUnprocessableEntity,
		Detail: detail,
		Errors: fieldErrors,
	}
}

// InternalError creates a 500 Internal Server Error problem detail.
func InternalError(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:   "about:blank",
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Detail: detail,
	}
}

// WriteJSON writes a ProblemDetail as a JSON response with the correct
// Content-Type and status code. It is a convenience helper used by all API handlers.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteProblem writes a ProblemDetail as a JSON response.
func WriteProblem(w http.ResponseWriter, p *ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

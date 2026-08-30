// Package api implements the REST management surface /api/v1 (a V1
// compatibility contract — additive only, docs/architecture/api.md): token
// auth with scopes, one error envelope, cursor pagination, idempotency keys,
// rate limits, durable webhook management, and hand-authored OpenAPI kept
// accurate by a route-coverage test.
package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Envelope is the single error shape of the API. Codes are the stable
// machine codes of internal/domain; messages are human-readable; request_id
// correlates with logs (never stack traces — security.md).
type Envelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// writeErr writes one error envelope.
func writeErr(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	reqID := RequestID(r.Context())
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Error: ErrorBody{
		Code: code, Message: message, RequestID: reqID,
	}})
}

// writeServiceErr maps any error to the envelope: domain errors carry their
// own code; everything else is INTERNAL_ERROR with the detail kept out of the
// response (it is logged upstream by the middleware).
func writeServiceErr(w http.ResponseWriter, r *http.Request, err error) {
	status, code := mapError(err)
	msg := "internal error"
	var de *domain.Error
	if errors.As(err, &de) {
		msg = de.Message
	}
	writeErr(w, r, status, code, msg)
}

// mapError converts domain machine codes to HTTP statuses.
func mapError(err error) (int, string) {
	code := domain.CodeOf(err)
	switch code {
	case domain.CodeInvalidRequest, domain.CodeSettingUnknown, domain.CodeSettingInvalid,
		domain.CodeSubnetInvalid, domain.CodeParamConstraint, domain.CodeConfigInvalid,
		domain.CodeTokenInvalid:
		return http.StatusBadRequest, code
	case domain.CodeCredentialInvalid:
		return http.StatusUnauthorized, code
	case domain.CodeForbidden, domain.CodeSessionExpired, domain.CodeOwnerProtected:
		return http.StatusForbidden, code
	case domain.CodeNotFound:
		return http.StatusNotFound, code
	case domain.CodeUserNotFound, domain.CodeDeviceNotFound, domain.CodePlanNotFound,
		domain.CodeInterfaceNotFound, domain.CodeAdminNotFound:
		return http.StatusNotFound, code
	case domain.CodeUsernameExists, domain.CodeDeviceLimitReached, domain.CodeDevicePoolExhausted,
		domain.CodeDeviceKeyExists, domain.CodePlanInUse, domain.CodeInterfaceNameTaken,
		domain.CodePortInUse, domain.CodeSubnetOverlap:
		return http.StatusConflict, code
	case domain.CodeRateLimited:
		return http.StatusTooManyRequests, code
	case domain.CodeNodeUnavailable:
		return http.StatusServiceUnavailable, code
	default:
		return http.StatusInternalServerError, domain.CodeInternal
	}
}

// notFound is the envelope for unmatched routes (mux root fallback).
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeErr(w, r, http.StatusNotFound, domain.CodeNotFound, "no such route: "+r.Method+" "+r.URL.Path)
}

// invalidRequestErr is the shorthand for handler-level 400s.
func invalidRequestErr(format string, args ...any) *domain.Error {
	return domain.E(domain.CodeInvalidRequest, format, args...)
}

// clientIP extracts the remote address (proxy deployments terminate TLS and
// set X-Forwarded-For upstream; WG-Guard trusts the socket peer).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

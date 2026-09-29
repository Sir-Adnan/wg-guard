package api

import (
	"net/http"
	"net/url"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/integration"
)

func integrationKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || values[0] == "" {
		writeErr(w, r, http.StatusBadRequest, domain.CodeInvalidRequest,
			"provide exactly one Idempotency-Key header")
		return "", false
	}
	return values[0], true
}

func (s *Server) handlePurchase(w http.ResponseWriter, r *http.Request) {
	key, ok := integrationKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Username   string `json:"username"`
		PlanID     string `json:"plan_id"`
		DeviceName string `json:"device_name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	keys, err := s.generateKeys(r, false)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	verified := TokenFrom(r.Context())
	result, replayed, err := s.Integration.Purchase(r.Context(), integration.PurchaseInput{
		Key: key, ResellerID: verified.Token.ResellerID, Username: req.Username,
		PlanID: req.PlanID, DeviceName: req.DeviceName, Keys: *keys,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	} else {
		s.audit(r, "integration.purchase_committed", result.ID,
			map[string]any{"user_id": result.UserID, "device_id": result.DeviceID})
		s.reconcile(r)
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleOperationResult(w http.ResponseWriter, r *http.Request) {
	key, ok := integrationKey(w, r)
	if !ok {
		return
	}
	verified := TokenFrom(r.Context())
	result, err := s.Integration.Lookup(r.Context(), verified.Token.ResellerID, key)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCustomerLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Users.Get(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	link, err := s.Links.ForUser(r.Context(), id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if link == nil || link.Revoked() {
		writeErr(w, r, http.StatusNotFound, domain.CodeNotFound, "subscription link not found")
		return
	}
	if link.Token == "" {
		writeErr(w, r, http.StatusServiceUnavailable, domain.CodeNodeUnavailable, "subscription link unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"path": "/sub/" + url.PathEscape(link.Token)})
}

func (s *Server) handleCustomerLinkRotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Users.Get(r.Context(), id); err != nil {
		writeServiceErr(w, r, err)
		return
	}
	link, count, err := s.Links.RotateAccess(r.Context(), s.Devices, id)
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	s.audit(r, "user.sub_revoked", id, map[string]any{"devices_rotated": count})
	if err := s.reconcile(r); err != nil {
		// The new credentials remain committed. Reversing them would revive
		// potentially leaked access; reconciliation is retryable.
		writeErr(w, r, http.StatusServiceUnavailable, domain.CodeNodeUnavailable,
			"access was rotated; runtime reconciliation is pending")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"path": "/sub/" + url.PathEscape(link.Token), "devices_rotated": count,
	})
}

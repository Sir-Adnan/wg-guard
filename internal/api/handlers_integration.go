package api

import (
	"net/http"
	"net/url"

	"github.com/Sir-Adnan/wg-guard/internal/device"
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
		Username    string                         `json:"username"`
		TemplateID  string                         `json:"template_id"`
		Entitlement *integration.DirectEntitlement `json:"entitlement"`
		DeviceName  string                         `json:"device_name"`
		DeviceCount *int                           `json:"device_count"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	count := 1
	if req.DeviceCount != nil {
		count = *req.DeviceCount
	}
	if count < 1 || count > integration.MaxPurchaseDevices {
		writeErr(w, r, http.StatusBadRequest, domain.CodeInvalidRequest, "device_count must be 1-100")
		return
	}
	deviceKeys := make([]device.KeyMaterial, 0, count)
	for i := 0; i < count; i++ {
		keys, err := s.generateKeys(r, false)
		if err != nil {
			writeServiceErr(w, r, err)
			return
		}
		deviceKeys = append(deviceKeys, *keys)
	}
	verified := TokenFrom(r.Context())
	result, replayed, err := s.Integration.Purchase(r.Context(), integration.PurchaseInput{
		Key: key, ResellerID: verified.Token.ResellerID, Username: req.Username,
		TemplateID: req.TemplateID, Entitlement: req.Entitlement, DeviceName: req.DeviceName, DeviceKeys: deviceKeys,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	} else {
		s.audit(r, "integration.purchase_committed", result.ID,
			map[string]any{"user_id": result.UserID, "device_count": len(result.DeviceIDs)})
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

func (s *Server) handleQuotaTopUp(w http.ResponseWriter, r *http.Request) {
	key, ok := integrationKey(w, r)
	if !ok {
		return
	}
	var req struct {
		Bytes int64 `json:"bytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	verified := TokenFrom(r.Context())
	result, replayed, err := s.Integration.TopUpQuota(r.Context(), integration.QuotaTopUpInput{
		Key: key, ResellerID: verified.Token.ResellerID, UserID: r.PathValue("id"), Bytes: req.Bytes,
	})
	if err != nil {
		writeServiceErr(w, r, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	} else {
		s.audit(r, "integration.quota_top_up", result.UserID, map[string]any{
			"operation_id": result.ID, "before_bytes": result.Before.TrafficLimitBytes,
			"after_bytes": result.After.TrafficLimitBytes,
		})
	}
	if result.Before.Status != result.After.Status {
		if err := s.reconcile(r); err != nil {
			writeErr(w, r, http.StatusServiceUnavailable, domain.CodeNodeUnavailable,
				"quota committed; runtime reconciliation is pending; inspect the operation result")
			return
		}
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

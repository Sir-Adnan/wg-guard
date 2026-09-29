package web

import (
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Webhooks screen (webhooks.read to view, webhooks.write to mutate). The
// signing secret is generated server-side and shown exactly once, on create
// or rotation.
type webhooksData struct {
	BasePath         string
	AllowOwnerFanout bool
	Form             operationalForm
	Selected         []string
	Known            bool
	DeliveryKnown    bool
	Show             *webhook.Endpoint
	List             []webhook.EndpointWithStats
	Delivery         []webhook.Delivery
	Catalog          []string

	Created struct {
		URL    string
		Secret string // shown exactly once
	}
}

func (s *Server) handleWebhooksPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "webhooks", "app", s.webhookPageData(r, nil))
}

func (s *Server) webhookPageData(r *http.Request, endpoint *webhook.Endpoint) webhooksData {
	d := webhooksData{BasePath: webhookBasePath(r), Show: endpoint, Catalog: webhook.Catalog(),
		AllowOwnerFanout: adminFrom(r).Role == auth.RoleOwner && (endpoint == nil || endpoint.ResellerID == nil),
		Form:             operationalForm{Values: map[string]string{"url": "", "enabled": "1", "include_reseller_events": ""}, Fields: map[string]string{}}}
	if adminFrom(r).ResellerID != nil {
		filtered := make([]string, 0, len(d.Catalog))
		for _, event := range d.Catalog {
			if event != webhook.EventNodeStarted {
				filtered = append(filtered, event)
			}
		}
		d.Catalog = filtered
	}
	if endpoint != nil {
		d.Form.Values["url"] = endpoint.URL
		d.Form.Values["enabled"] = ""
		if endpoint.Enabled {
			d.Form.Values["enabled"] = "1"
		}
		if endpoint.IncludeResellerEvents {
			d.Form.Values["include_reseller_events"] = "1"
		}
		d.Selected = endpoint.Events
		var err error
		if canOperate(r, "webhooks.read") {
			if resellerID := adminFrom(r).ResellerID; resellerID != nil {
				var receipts []webhook.Receipt
				receipts, _, err = s.Webhooks.Receipts(r.Context(), resellerID, endpoint.ID, 50, "", "")
				d.Delivery = safePanelDeliveries(receipts)
			} else if webhookPanelGlobalOnly(r) {
				var receipts []webhook.Receipt
				receipts, _, err = s.Webhooks.ReceiptsGlobal(r.Context(), endpoint.ID, 50, "", "")
				d.Delivery = safePanelDeliveries(receipts)
			} else {
				d.Delivery, err = s.Webhooks.Deliveries(r.Context(), endpoint.ID, 50)
			}
			d.DeliveryKnown = err == nil
		}
	} else {
		var err error
		if canOperate(r, "webhooks.read") {
			if resellerID := adminFrom(r).ResellerID; resellerID != nil {
				d.List, err = s.Webhooks.ListFor(r.Context(), *resellerID)
			} else if webhookPanelGlobalOnly(r) {
				d.List, err = s.Webhooks.ListGlobal(r.Context())
			} else {
				d.List, err = s.Webhooks.List(r.Context())
			}
			d.Known = err == nil
		}
	}
	return d
}

func safePanelDeliveries(receipts []webhook.Receipt) []webhook.Delivery {
	var deliveries []webhook.Delivery
	for _, receipt := range receipts {
		created, _ := time.Parse(time.RFC3339Nano, receipt.CreatedAt)
		deliveries = append(deliveries, webhook.Delivery{ID: receipt.ID, EventType: receipt.EventType,
			Status: receipt.Status, Attempts: receipt.Attempts, CreatedAt: created})
	}
	return deliveries
}

func webhookBasePath(r *http.Request) string {
	if adminFrom(r).ResellerID != nil {
		return "/reseller/webhooks"
	}
	return "/webhooks"
}

func webhookPanelGlobalOnly(r *http.Request) bool {
	a := adminFrom(r)
	return a != nil && a.ResellerID == nil && a.Role != auth.RoleOwner
}

func (s *Server) webhookGet(r *http.Request, id string) (*webhook.Endpoint, error) {
	if resellerID := adminFrom(r).ResellerID; resellerID != nil {
		return s.Webhooks.GetFor(r.Context(), *resellerID, id)
	}
	if webhookPanelGlobalOnly(r) {
		return s.Webhooks.GetGlobal(r.Context(), id)
	}
	return s.Webhooks.Get(r.Context(), id)
}

func (s *Server) webhookFormFailure(w http.ResponseWriter, r *http.Request, id string, err error) {
	var endpoint *webhook.Endpoint
	if id != "" {
		endpoint, _ = s.webhookGet(r, id)
		if endpoint == nil {
			endpoint = &webhook.Endpoint{ID: id}
		}
	}
	d := s.webhookPageData(r, endpoint)
	d.Form = submittedOperationalForm(r, d.Form.Values)
	// URL user information is a rejected credential, never a retained form value.
	if parsed, parseErr := neturl.Parse(d.Form.V("url")); parseErr == nil {
		if parsed.User != nil {
			parsed.User = nil
			d.Form.Values["url"] = parsed.String()
		}
	} else {
		// Parsing can fail inside the password itself (for example %zz).
		// Inspect only the apparent authority; never reflect rejected credentials.
		raw := strings.TrimSpace(d.Form.V("url"))
		_, authority, hasScheme := strings.Cut(raw, "://")
		if !hasScheme {
			authority = strings.TrimPrefix(raw, "//")
		}
		if end := strings.IndexAny(authority, "/?#"); end >= 0 {
			authority = authority[:end]
		}
		if strings.Contains(authority, "@") {
			d.Form.Values["url"] = ""
		}
	}
	d.Selected = r.Form["events"]
	if domain.CodeOf(err) == domain.CodeInvalidRequest {
		field := "url"
		if strings.Contains(err.Error(), "event") {
			field = "events"
		}
		d.Form.Fields[field] = "common.error_validation"
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	_ = s.render(w, r, "webhooks", "app", d)
}

// handleWebhookCreate adds an endpoint; the (generated) signing secret is
// rendered once on the response page.
func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.PostFormValue("url"))
	events := r.Form["events"]
	var e *webhook.Endpoint
	var secret string
	var err error
	if adminFrom(r).Role == auth.RoleOwner {
		e, secret, err = s.Webhooks.CreateOwner(r.Context(), r.PostFormValue("include_reseller_events") == "1", url, events, "")
	} else {
		e, secret, err = s.Webhooks.CreateFor(r.Context(), adminFrom(r).ResellerID, url, events, "")
	}
	if err != nil {
		s.webhookFormFailure(w, r, "", err)
		return
	}
	s.audit(r, "webhooks.created", e.ID, nil)
	d := s.webhookPageData(r, nil)
	d.Created.URL = e.URL
	d.Created.Secret = secret
	_ = s.render(w, r, "webhooks", "app", d)
}

// handleWebhookShow is the endpoint detail: recent deliveries + actions.
func (s *Server) handleWebhookShow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, err := s.webhookGet(r, id)
	if err != nil {
		s.opsError(w, r, webhookBasePath(r), err)
		return
	}
	_ = s.render(w, r, "webhooks", "app", s.webhookPageData(r, e))
}

// handleWebhookUpdate applies the complete endpoint settings form.
func (s *Server) handleWebhookUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	url := strings.TrimSpace(r.PostFormValue("url"))
	enabled := r.PostFormValue("enabled") == "1"
	// This is a complete HTML form, so unchecked events mean an explicit empty
	// selection. The service reserves nil for omitted fields in partial updates.
	events := append([]string{}, r.Form["events"]...)
	in := webhook.EndpointUpdate{URL: &url, Events: events, Enabled: &enabled}
	if adminFrom(r).Role == auth.RoleOwner {
		if e, err := s.webhookGet(r, id); err == nil && e.ResellerID == nil {
			value := r.PostFormValue("include_reseller_events") == "1"
			in.IncludeResellerEvents = &value
		}
	}
	var err error
	if resellerID := adminFrom(r).ResellerID; resellerID != nil {
		_, _, err = s.Webhooks.UpdateFor(r.Context(), *resellerID, id, in)
	} else if webhookPanelGlobalOnly(r) {
		_, _, err = s.Webhooks.UpdateGlobal(r.Context(), id, in)
	} else {
		_, _, err = s.Webhooks.Update(r.Context(), id, in)
	}
	if err != nil {
		s.webhookFormFailure(w, r, id, err)
		return
	}
	s.audit(r, "webhooks.updated", id, nil)
	s.redirectToast(w, r, operationalReturnPath(r, webhookBasePath(r)+"/"+id, "webhooks.read"), "hooks.toast.updated")
}

// handleWebhookRotate generates a new signing secret (shown once).
func (s *Server) handleWebhookRotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	empty := ""
	var e *webhook.Endpoint
	var secret string
	var err error
	if resellerID := adminFrom(r).ResellerID; resellerID != nil {
		e, secret, err = s.Webhooks.UpdateFor(r.Context(), *resellerID, id, webhook.EndpointUpdate{Secret: &empty})
	} else if webhookPanelGlobalOnly(r) {
		e, secret, err = s.Webhooks.UpdateGlobal(r.Context(), id, webhook.EndpointUpdate{Secret: &empty})
	} else {
		e, secret, err = s.Webhooks.Update(r.Context(), id, webhook.EndpointUpdate{Secret: &empty})
	}
	if err != nil {
		s.opsError(w, r, webhookBasePath(r)+"/"+id, err)
		return
	}
	s.audit(r, "webhooks.secret_rotated", e.ID, nil)
	d := s.webhookPageData(r, e)
	d.Created.URL = e.URL
	d.Created.Secret = secret
	_ = s.render(w, r, "webhooks", "app", d)
}

// handleWebhookDelete removes the endpoint (deliveries cascade).
func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	if resellerID := adminFrom(r).ResellerID; resellerID != nil {
		err = s.Webhooks.DeleteFor(r.Context(), *resellerID, id)
	} else if webhookPanelGlobalOnly(r) {
		err = s.Webhooks.DeleteGlobal(r.Context(), id)
	} else {
		err = s.Webhooks.Delete(r.Context(), id)
	}
	if err != nil {
		s.opsError(w, r, webhookBasePath(r), err)
		return
	}
	s.audit(r, "webhooks.deleted", id, nil)
	s.redirectToast(w, r, webhookBasePath(r), "hooks.toast.deleted")
}

// handleWebhookRedeliver requeues one delivery.
func (s *Server) handleWebhookRedeliver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deliveryID := r.PostFormValue("delivery_id")
	var err error
	if resellerID := adminFrom(r).ResellerID; resellerID != nil {
		err = s.Webhooks.RedeliverFor(r.Context(), *resellerID, id, deliveryID)
	} else if webhookPanelGlobalOnly(r) {
		err = s.Webhooks.RedeliverGlobal(r.Context(), id, deliveryID)
	} else {
		err = s.Webhooks.Redeliver(r.Context(), id, deliveryID)
	}
	if err != nil {
		s.opsError(w, r, webhookBasePath(r)+"/"+id, err)
		return
	}
	s.audit(r, "webhooks.redelivered", deliveryID, nil)
	s.redirectToast(w, r, webhookBasePath(r)+"/"+id, "hooks.toast.redeliver")
}

package web

import (
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Webhooks screen (webhooks.read to view, webhooks.write to mutate). The
// signing secret is generated server-side and shown exactly once, on create
// or rotation.
type webhooksData struct {
	Form          operationalForm
	Selected      []string
	Known         bool
	DeliveryKnown bool
	Show          *webhook.Endpoint
	List          []webhook.EndpointWithStats
	Delivery      []webhook.Delivery
	Catalog       []string

	Created struct {
		URL    string
		Secret string // shown exactly once
	}
}

func (s *Server) handleWebhooksPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "webhooks", "app", s.webhookPageData(r, nil))
}

func (s *Server) webhookPageData(r *http.Request, endpoint *webhook.Endpoint) webhooksData {
	d := webhooksData{Show: endpoint, Catalog: webhook.Catalog(), Form: operationalForm{Values: map[string]string{"url": "", "enabled": "1"}, Fields: map[string]string{}}}
	if endpoint != nil {
		d.Form.Values["url"] = endpoint.URL
		d.Form.Values["enabled"] = ""
		if endpoint.Enabled {
			d.Form.Values["enabled"] = "1"
		}
		d.Selected = endpoint.Events
		var err error
		if canOperate(r, "webhooks.read") {
			d.Delivery, err = s.Webhooks.Deliveries(r.Context(), endpoint.ID, 50)
			d.DeliveryKnown = err == nil
		}
	} else {
		var err error
		if canOperate(r, "webhooks.read") {
			d.List, err = s.Webhooks.List(r.Context())
			d.Known = err == nil
		}
	}
	return d
}

func (s *Server) webhookFormFailure(w http.ResponseWriter, r *http.Request, id string, err error) {
	var endpoint *webhook.Endpoint
	if id != "" {
		endpoint, _ = s.Webhooks.Get(r.Context(), id)
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
	e, secret, err := s.Webhooks.Create(r.Context(), url, events, "")
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
	e, err := s.Webhooks.Get(r.Context(), id)
	if err != nil {
		s.opsError(w, r, "/webhooks", err)
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
	if _, _, err := s.Webhooks.Update(r.Context(), id, in); err != nil {
		s.webhookFormFailure(w, r, id, err)
		return
	}
	s.audit(r, "webhooks.updated", id, nil)
	s.redirectToast(w, r, operationalReturnPath(r, "/webhooks/"+id, "webhooks.read"), "hooks.toast.updated")
}

// handleWebhookRotate generates a new signing secret (shown once).
func (s *Server) handleWebhookRotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	empty := ""
	e, secret, err := s.Webhooks.Update(r.Context(), id, webhook.EndpointUpdate{Secret: &empty})
	if err != nil {
		s.opsError(w, r, "/webhooks/"+id, err)
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
	if err := s.Webhooks.Delete(r.Context(), id); err != nil {
		s.opsError(w, r, "/webhooks", err)
		return
	}
	s.audit(r, "webhooks.deleted", id, nil)
	s.redirectToast(w, r, "/webhooks", "hooks.toast.deleted")
}

// handleWebhookRedeliver requeues one delivery.
func (s *Server) handleWebhookRedeliver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deliveryID := r.PostFormValue("delivery_id")
	if err := s.Webhooks.Redeliver(r.Context(), id, deliveryID); err != nil {
		s.opsError(w, r, "/webhooks/"+id, err)
		return
	}
	s.audit(r, "webhooks.redelivered", deliveryID, nil)
	s.redirectToast(w, r, "/webhooks/"+id, "hooks.toast.redeliver")
}

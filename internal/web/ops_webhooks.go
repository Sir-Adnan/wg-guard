package web

import (
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

// Webhooks screen (webhooks.read to view, webhooks.write to mutate). The
// signing secret is generated server-side and shown exactly once, on create
// or rotation.
type webhooksData struct {
	Error    string
	Show     *webhook.Endpoint
	List     []webhook.EndpointWithStats
	Delivery []webhook.Delivery
	Catalog  []string

	Created struct {
		URL    string
		Secret string // shown exactly once
	}
}

func (s *Server) handleWebhooksPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.Webhooks.List(r.Context())
	if err != nil {
		s.logError(r, "webhooks list", err)
	}
	_ = s.render(w, r, "webhooks", "app", webhooksData{List: list, Catalog: webhook.Catalog()})
}

// handleWebhookCreate adds an endpoint; the (generated) signing secret is
// rendered once on the response page.
func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.PostFormValue("url"))
	events := r.Form["events"]
	e, secret, err := s.Webhooks.Create(r.Context(), url, events, "")
	if err != nil {
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			d := webhooksData{List: s.webhookList(r), Catalog: webhook.Catalog()}
			d.Error = err.Error()
			_ = s.render(w, r, "webhooks", "app", d)
			return
		}
		s.logError(r, "webhook create", err)
		s.redirectToast(w, r, "/webhooks", "common.error_generic")
		return
	}
	s.audit(r, "webhooks.created", e.URL, nil)
	d := webhooksData{List: s.webhookList(r), Catalog: webhook.Catalog()}
	d.Created.URL = e.URL
	d.Created.Secret = secret
	_ = s.render(w, r, "webhooks", "app", d)
}

func (s *Server) webhookList(r *http.Request) []webhook.EndpointWithStats {
	list, err := s.Webhooks.List(r.Context())
	if err != nil {
		return nil
	}
	return list
}

// handleWebhookShow is the endpoint detail: recent deliveries + actions.
func (s *Server) handleWebhookShow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, err := s.Webhooks.Get(r.Context(), id)
	if err != nil {
		s.opsError(w, r, "/webhooks", err)
		return
	}
	deliveries, err := s.Webhooks.Deliveries(r.Context(), id, 50)
	if err != nil {
		s.logError(r, "webhook deliveries", err)
	}
	_ = s.render(w, r, "webhooks", "app", webhooksData{
		Show: e, Delivery: deliveries, Catalog: webhook.Catalog(),
	})
}

// handleWebhookUpdate applies URL/events/enabled changes from the edit modal.
func (s *Server) handleWebhookUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	url := strings.TrimSpace(r.PostFormValue("url"))
	enabled := r.PostFormValue("enabled") == "1"
	in := webhook.EndpointUpdate{URL: &url, Events: r.Form["events"], Enabled: &enabled}
	if _, _, err := s.Webhooks.Update(r.Context(), id, in); err != nil {
		s.opsError(w, r, "/webhooks/"+id, err)
		return
	}
	s.audit(r, "webhooks.updated", url, nil)
	s.redirectToast(w, r, "/webhooks", "hooks.toast.updated")
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
	s.audit(r, "webhooks.secret_rotated", e.URL, nil)
	deliveries, _ := s.Webhooks.Deliveries(r.Context(), id, 50)
	d := webhooksData{Show: e, Delivery: deliveries, Catalog: webhook.Catalog()}
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

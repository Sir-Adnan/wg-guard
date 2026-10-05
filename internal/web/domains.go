package web

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domainqueue"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

type domainPageData struct {
	Inventory                    install.DomainInventory
	Cards                        []domainCard
	Status                       domainqueue.Status
	Available, Active, CanManage bool
	Error                        string
	PanelTarget                  string
	Form                         domainqueue.Input
}
type domainCard struct {
	Role                                      domaintls.Role
	Title, Description, Origin, Method, State string
	Fingerprint                               string
	Expires                                   *time.Time
	CanRenew, CanRemove                       bool
}

func (s *Server) domainData(r *http.Request) domainPageData {
	d := domainPageData{CanManage: adminFrom(r) != nil && adminFrom(r).Role == auth.RoleOwner, Form: domainqueue.Input{Operation: "configure", Role: domaintls.Subscription, Method: domaintls.Automatic, Challenge: "http"}}
	if s.DomainQueue != nil {
		d.Available = s.DomainQueue.Ready()
		if s.DomainQueue.ReadInventory(&d.Inventory) != nil || d.Inventory.Schema != 1 || d.Inventory.Revision != "legacy" && !domaintls.IDPattern.MatchString(d.Inventory.Revision) {
			d.Inventory = install.DomainInventory{}
			d.Available = false
		}
		d.Status, _ = s.DomainQueue.Status()
		d.Active = d.Status.State == "queued" || d.Status.State == "running"
		d.Available = d.Available && d.Inventory.Available
	}
	d.Form.ExpectedRevision = d.Inventory.Revision
	if d.Status.Role == domaintls.Panel && d.Status.Origin != "" {
		d.PanelTarget = d.Status.Origin
	}
	for _, role := range []domaintls.Role{domaintls.Panel, domaintls.Subscription} {
		card := domainCard{Role: role, Title: "domains.role." + string(role), Description: "domains.description." + string(role), State: "unavailable"}
		if role == domaintls.Panel {
			card.Origin = d.Inventory.PanelOrigin
		} else {
			card.Origin = d.Inventory.SubscriptionOrigin
		}
		if card.Origin != "" {
			card.State = "configured"
		}
		for _, certificate := range d.Inventory.Certificates {
			if certificate.Site.Role == role {
				card.CanRemove = role == domaintls.Subscription
				card.Method = string(certificate.Site.Method)
				card.State = certificate.State
				card.Fingerprint = certificate.Info.Fingerprint
				card.CanRenew = certificate.Site.Method == domaintls.Automatic
				if !certificate.Info.NotAfter.IsZero() {
					at := certificate.Info.NotAfter
					card.Expires = &at
				}
			}
		}
		d.Cards = append(d.Cards, card)
	}
	return d
}

func (s *Server) handleDomainsPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "domains", "app", s.domainData(r))
}
func (s *Server) handleDomainsStatus(w http.ResponseWriter, r *http.Request) {
	d := s.domainData(r)
	w.Header().Set("Cache-Control", "no-store")
	if !d.Active && r.URL.Query().Get("id") == d.Status.ID && (r.URL.Query().Get("state") == "running" || r.URL.Query().Get("state") == "queued") {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Query().Get("id") == d.Status.ID && r.URL.Query().Get("state") == d.Status.State && r.URL.Query().Get("stage") == d.Status.Stage {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(204)
		return
	}
	_ = s.partial(w, r, "domains", "domain-status", d)
}

func (s *Server) handleDomainsRequest(w http.ResponseWriter, r *http.Request) {
	d := s.domainData(r)
	if !d.Available || s.DomainQueue == nil {
		s.domainError(w, r, d, "domains.error.unavailable")
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			s.domainError(w, r, d, "domains.error.invalid")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
	}
	input := domainqueue.Input{Operation: r.PostFormValue("operation"), Role: domaintls.Role(r.PostFormValue("role")), Origin: strings.TrimSpace(r.PostFormValue("origin")), Method: domaintls.Method(r.PostFormValue("method")), ExpectedRevision: r.PostFormValue("expected_revision"), Challenge: r.PostFormValue("challenge"), Email: strings.TrimSpace(r.PostFormValue("email"))}
	if input.Operation == "inspect" || input.Operation == "recover" {
		input = domainqueue.Input{Operation: input.Operation}
	}
	if input.Operation == "configure" {
		d.Form = input
		origin, err := domaintls.ParseOrigin(input.Origin)
		if err != nil {
			d.Form = input
			s.domainError(w, r, d, "domains.error.origin")
			return
		}
		input.Origin = origin.URL
		if input.Method == domaintls.Manual {
			input.Challenge, input.Email = "", ""
			if r.PostFormValue("source") == "paths" {
				input.CertSource, input.KeySource = r.PostFormValue("cert_source"), r.PostFormValue("key_source")
			} else {
				cert, err := domainUpload(r, "certificate")
				if err != nil {
					s.domainError(w, r, d, "domains.error.material")
					return
				}
				defer clear(cert)
				key, err := domainUpload(r, "private_key")
				if err != nil {
					s.domainError(w, r, d, "domains.error.material")
					return
				}
				defer clear(key)
				input.StageID, err = s.DomainQueue.Stage(cert, key)
				if err != nil {
					s.domainError(w, r, d, "domains.error.material")
					return
				}
			}
		} else if input.Method == domaintls.External {
			input.Challenge, input.Email = "", ""
		}
	}
	status, err := s.DomainQueue.Enqueue(input, adminFrom(r).ID)
	if err != nil {
		if input.StageID != "" {
			_ = s.DomainQueue.RemoveImport(input.StageID)
		}
		d.Form = input
		s.domainError(w, r, d, "domains.error.request")
		return
	}
	s.audit(r, "domains.requested", status.ID, map[string]any{"operation": input.Operation, "role": input.Role, "method": input.Method})
	s.redirectToast(w, r, "/settings/domains", "domains.toast.queued")
}

func domainUpload(r *http.Request, name string) ([]byte, error) {
	f, _, err := r.FormFile(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	bytes, err := io.ReadAll(io.LimitReader(f, domaintls.MaxMaterialBytes+1))
	if err != nil || len(bytes) == 0 || len(bytes) > domaintls.MaxMaterialBytes {
		clear(bytes)
		return nil, fmt.Errorf("invalid certificate upload")
	}
	return bytes, nil
}

func (s *Server) domainError(w http.ResponseWriter, r *http.Request, d domainPageData, key string) {
	d.Error = s.t(r, key)
	w.WriteHeader(http.StatusBadRequest)
	_ = s.render(w, r, "domains", "app", d)
}

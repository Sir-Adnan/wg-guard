package web

import (
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

type resellerCard struct {
	Account reseller.Account
	Admins  []admin.Admin
	PlanIDs []string
}

type resellerPlanOption struct{ ID, Name string }

type resellersData struct {
	Cards    []resellerCard
	Known    bool
	Scopes   []string
	Plans    []resellerPlanOption
	Selected []string
	Form     operationalForm
}

func (s *Server) resellerPageData(r *http.Request) resellersData {
	d := resellersData{Known: true, Scopes: auth.ResellerScopes(),
		Form: operationalForm{Values: map[string]string{"slug": "", "display_name": ""}, Fields: map[string]string{}}}
	accounts, err := s.Resellers.List(r.Context())
	if err != nil {
		d.Known = false
		s.logError(r, "reseller list unavailable", nil)
		return d
	}
	admins, err := s.Admins.List(r.Context())
	if err != nil {
		d.Known = false
		s.logError(r, "reseller admins unavailable", nil)
		return d
	}
	plans, err := s.Plans.List(r.Context())
	if err != nil {
		d.Known = false
		s.logError(r, "reseller plans unavailable", nil)
		return d
	}
	for _, p := range plans {
		if p.Enabled {
			d.Plans = append(d.Plans, resellerPlanOption{ID: p.ID, Name: p.Name})
		}
	}
	for _, account := range accounts {
		card := resellerCard{Account: account}
		card.PlanIDs, err = s.Resellers.Plans(r.Context(), account.ID)
		if err != nil {
			d.Known = false
			s.logError(r, "reseller plan assignments unavailable", nil)
			return d
		}
		for _, a := range admins {
			if a.ResellerID != nil && *a.ResellerID == account.ID {
				card.Admins = append(card.Admins, a)
			}
		}
		d.Cards = append(d.Cards, card)
	}
	return d
}

func (s *Server) handleResellersPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "resellers", "app", s.resellerPageData(r))
}

func (s *Server) handleResellerCreate(w http.ResponseWriter, r *http.Request) {
	account, err := s.Resellers.Create(r.Context(), r.PostFormValue("slug"),
		r.PostFormValue("display_name"), r.Form["permissions"])
	if err != nil {
		d := s.resellerPageData(r)
		d.Form = submittedOperationalForm(r, d.Form.Values)
		d.Selected = r.Form["permissions"]
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			d.Form.Fields["slug"] = "common.error_validation"
		}
		s.operationalFormStatus(w, r, &d.Form, err)
		_ = s.render(w, r, "resellers", "app", d)
		return
	}
	s.audit(r, "resellers.created", account.ID, map[string]any{"slug": account.Slug})
	s.redirectToast(w, r, "/resellers", "resellers.saved")
}

func (s *Server) handleResellerPermissions(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.surfaceError(w, r, http.StatusBadRequest, "common.error_validation", "")
		return
	}
	id := r.PathValue("id")
	if err := s.Resellers.SetPermissions(r.Context(), id, r.Form["permissions"]); err != nil {
		s.opsError(w, r, "/resellers", err)
		return
	}
	s.audit(r, "resellers.permissions_updated", id, map[string]any{"count": len(r.Form["permissions"])})
	s.redirectToast(w, r, "/resellers", "resellers.saved")
}

func (s *Server) handleResellerPlans(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.surfaceError(w, r, http.StatusBadRequest, "common.error_validation", "")
		return
	}
	id := r.PathValue("id")
	if err := s.Resellers.SetPlans(r.Context(), id, r.Form["plans"]); err != nil {
		s.opsError(w, r, "/resellers", err)
		return
	}
	s.audit(r, "resellers.plans_updated", id, map[string]any{"count": len(r.Form["plans"])})
	s.redirectToast(w, r, "/resellers", "resellers.saved")
}

func (s *Server) handleResellerEnable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	enabled := r.PostFormValue("enable") == "1"
	if err := s.Resellers.SetEnabled(r.Context(), id, enabled); err != nil {
		s.opsError(w, r, "/resellers", err)
		return
	}
	s.audit(r, "resellers.enabled_changed", id, map[string]any{"enabled": enabled})
	s.redirectToast(w, r, "/resellers", "resellers.saved")
}

func (s *Server) handleResellerAdminCreate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.Admins.CreateForReseller(r.Context(), id,
		strings.TrimSpace(r.PostFormValue("username")), r.PostFormValue("password"), r.Form["permissions"])
	if err != nil {
		s.opsError(w, r, "/resellers", err)
		return
	}
	s.audit(r, "resellers.admin_created", id, map[string]any{"admin_id": a.ID})
	s.redirectToast(w, r, "/resellers", "resellers.saved")
}

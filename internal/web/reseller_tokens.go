package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
	"github.com/Sir-Adnan/wg-guard/internal/token"
)

type resellerTokensData struct {
	Account  *reseller.Account
	Tokens   []token.Token
	Scopes   []string
	Base     string
	Known    bool
	Form     operationalForm
	Selected []string
	Created  struct{ Name, Secret string }
}

func resellerTokenTarget(r *http.Request) (id, base string) {
	if a := adminFrom(r); a != nil && a.ResellerID != nil {
		return *a.ResellerID, "/reseller/tokens"
	}
	id = r.PathValue("id")
	return id, "/resellers/" + id + "/tokens"
}

func (s *Server) resellerTokensData(r *http.Request) (resellerTokensData, error) {
	id, base := resellerTokenTarget(r)
	d := resellerTokensData{Base: base, Known: true,
		Form: operationalForm{Values: map[string]string{"name": "", "expires_days": "0", "cidr": ""}, Fields: map[string]string{}}}
	a, err := s.Resellers.Get(r.Context(), id)
	if err != nil {
		return d, err
	}
	d.Account = a
	d.Tokens, err = s.Tokens.ListForReseller(r.Context(), id)
	if err != nil {
		return d, err
	}
	for _, scope := range auth.ResellerScopes() {
		if scope != auth.ScopeAPITokensManage && auth.Allows(a.Permissions, scope) {
			d.Scopes = append(d.Scopes, scope)
		}
	}
	return d, nil
}

func (s *Server) handleResellerTokensPage(w http.ResponseWriter, r *http.Request) {
	d, err := s.resellerTokensData(r)
	if err != nil {
		s.resellerTokensError(w, r, err)
		return
	}
	_ = s.render(w, r, "reseller_tokens", "app", d)
}

func (s *Server) resellerTokensError(w http.ResponseWriter, r *http.Request, err error) {
	if domain.CodeOf(err) == domain.CodeNotFound {
		s.surfaceError(w, r, http.StatusNotFound, "common.error_not_found", "")
		return
	}
	s.logError(r, "reseller tokens unavailable", nil)
	s.surfaceError(w, r, http.StatusServiceUnavailable, "common.error_generic", "")
}

func (s *Server) resellerTokenFormFailure(w http.ResponseWriter, r *http.Request, err error) {
	d, loadErr := s.resellerTokensData(r)
	if loadErr != nil {
		s.resellerTokensError(w, r, loadErr)
		return
	}
	d.Form = submittedOperationalForm(r, d.Form.Values)
	d.Selected = r.Form["scopes"]
	s.operationalFormStatus(w, r, &d.Form, err)
	_ = s.render(w, r, "reseller_tokens", "app", d)
}

func (s *Server) handleResellerTokenCreate(w http.ResponseWriter, r *http.Request) {
	id, _ := resellerTokenTarget(r)
	name := strings.TrimSpace(r.PostFormValue("name"))
	if len(r.Form["scopes"]) == 0 {
		s.resellerTokenFormFailure(w, r, domain.E(domain.CodeInvalidRequest, "token scopes are required"))
		return
	}
	var expires *time.Time
	if raw := strings.TrimSpace(r.PostFormValue("expires_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 0 || days > 3650 {
			s.resellerTokenFormFailure(w, r, domain.E(domain.CodeInvalidRequest, "invalid expiry"))
			return
		}
		if days > 0 {
			t := time.Now().AddDate(0, 0, days)
			expires = &t
		}
	}
	var target *string
	if adminFrom(r).ResellerID == nil {
		target = &id // owner creates on behalf of this reseller
	}
	created, secret, err := s.Tokens.CreateForAdmin(r.Context(), adminFrom(r).ID, target,
		name, r.Form["scopes"], expires, strings.TrimSpace(r.PostFormValue("cidr")))
	if err != nil {
		s.resellerTokenFormFailure(w, r, err)
		return
	}
	s.audit(r, "resellers.token_created", id, map[string]any{"token_id": created.ID})
	d, err := s.resellerTokensData(r)
	if err != nil {
		s.resellerTokensError(w, r, err)
		return
	}
	d.Created.Name, d.Created.Secret = created.Name, secret
	_ = s.render(w, r, "reseller_tokens", "app", d)
}

func (s *Server) handleResellerTokenRevoke(w http.ResponseWriter, r *http.Request) {
	id, base := resellerTokenTarget(r)
	if err := s.Tokens.RevokeForReseller(r.Context(), r.PathValue("tokenID"), id); err != nil {
		s.opsError(w, r, base, err)
		return
	}
	s.audit(r, "resellers.token_revoked", id, map[string]any{"token_id": r.PathValue("tokenID")})
	s.redirectToast(w, r, base, "tokens.toast.revoked")
}

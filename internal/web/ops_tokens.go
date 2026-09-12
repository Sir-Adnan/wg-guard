package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/token"
)

// API tokens screen (api_tokens.manage). The minted plaintext is rendered
// exactly once from the create response — never stored in flash state.
type tokensData struct {
	Form     operationalForm
	Selected []string
	Known    bool
	Tokens   []token.Token
	ScopeSet []scopeGroup

	Created struct {
		Name   string
		Secret string // shown exactly once
	}
}

func (s *Server) handleTokensPage(w http.ResponseWriter, r *http.Request) {
	d := s.tokensData(r)
	_ = s.render(w, r, "tokens", "app", d)
}

func (s *Server) tokensData(r *http.Request) tokensData {
	list, err := s.Tokens.List(r.Context())
	if err != nil {
		s.logError(r, "tokens list", err)
	}
	return tokensData{Known: err == nil, Tokens: list, ScopeSet: scopeGroups(), Form: operationalForm{Values: map[string]string{"name": "", "expires_days": "0", "cidr": ""}, Fields: map[string]string{}}}
}

func (s *Server) tokenFormFailure(w http.ResponseWriter, r *http.Request, field string, err error) {
	d := s.tokensData(r)
	d.Form = submittedOperationalForm(r, d.Form.Values)
	d.Selected = r.Form["scopes"]
	if field != "" {
		d.Form.Fields[field] = "common.error_validation"
	}
	if domain.CodeOf(err) == domain.CodeInvalidRequest {
		for _, pair := range [][2]string{{"token name", "name"}, {"scopes:", "scopes"}, {"cidr allowlist:", "cidr"}} {
			if strings.HasPrefix(err.Error(), pair[0]) {
				d.Form.Fields[pair[1]] = "common.error_validation"
			}
		}
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	_ = s.render(w, r, "tokens", "app", d)
}

// handleTokenCreate mints a token and renders the show-once secret.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	scopes := r.Form["scopes"]
	var expires *time.Time
	if raw := strings.TrimSpace(r.PostFormValue("expires_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days < 0 || days > 3650 {
			s.tokenFormFailure(w, r, "expires_days", errInvalid)
			return
		}
		if days > 0 {
			t := time.Now().AddDate(0, 0, days)
			expires = &t
		}
	}
	cidr := strings.TrimSpace(r.PostFormValue("cidr"))

	created, secret, err := s.Tokens.Create(r.Context(), name, scopes, expires, cidr)
	if err != nil {
		s.tokenFormFailure(w, r, "", err)
		return
	}
	s.audit(r, "tokens.created", created.Name, map[string]any{"scopes": len(scopes)})
	d := s.tokensData(r)
	d.Created.Name = created.Name
	d.Created.Secret = secret
	_ = s.render(w, r, "tokens", "app", d)
}

// handleTokenRevoke disables a token (rows stay for audit).
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Tokens.Revoke(r.Context(), id); err != nil {
		s.opsError(w, r, "/tokens", err)
		return
	}
	s.audit(r, "tokens.revoked", id, nil)
	s.redirectToast(w, r, "/tokens", "tokens.toast.revoked")
}

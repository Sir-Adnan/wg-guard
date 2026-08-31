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
	Error string
	Field string

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
	return tokensData{Tokens: list, ScopeSet: scopeGroups()}
}

// handleTokenCreate mints a token and renders the show-once secret.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	scopes := r.Form["scopes"]
	var expires *time.Time
	if days, err := strconv.Atoi(r.PostFormValue("expires_days")); err == nil && days > 0 {
		t := time.Now().AddDate(0, 0, days)
		expires = &t
	}
	cidr := strings.TrimSpace(r.PostFormValue("cidr"))

	created, secret, err := s.Tokens.Create(r.Context(), name, scopes, expires, cidr)
	if err != nil {
		switch domain.CodeOf(err) {
		case domain.CodeInvalidRequest, domain.CodeTokenExists:
			d := s.tokensData(r)
			d.Error = err.Error()
			d.Field = "name"
			_ = s.render(w, r, "tokens", "app", d)
		default:
			s.logError(r, "token create", err)
			s.redirectToast(w, r, "/tokens", "common.error_generic")
		}
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

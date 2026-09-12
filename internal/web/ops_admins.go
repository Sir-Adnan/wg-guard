package web

import (
	"net/http"
	"sort"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// Administrators screen (admins.manage). Secret minted here: none — only
// password resets, which are fire-and-forget. Owner protection (the last
// owner cannot be removed/demoted) is enforced by the admin service.
type adminsData struct {
	Form     operationalForm
	Known    bool
	Editing  *admin.Admin
	Selected []string
	Admins   []admin.Admin
	ScopeSet []scopeGroup // permission matrix for create/edit
}

type scopeGroup struct {
	Family string
	Scopes []string
}

// scopeGroups clusters the registry scopes into labeled families for the
// permission matrix (stable order for rendering and tests).
func scopeGroups() []scopeGroup {
	families := map[string][]string{}
	for _, sc := range auth.AllScopes() {
		fam := sc
		if i := strings.Index(sc, "."); i > 0 {
			fam = sc[:i]
		}
		families[fam] = append(families[fam], sc)
	}
	order := []string{"users", "devices", "configs", "traffic", "plans", "interfaces",
		"stats", "node", "webhooks", "audit", "api_tokens", "admins", "server", "backup", "update"}
	out := make([]scopeGroup, 0, len(families))
	seen := map[string]bool{}
	for _, fam := range order {
		if scopes, ok := families[fam]; ok {
			out = append(out, scopeGroup{Family: fam, Scopes: append([]string{fam + ".*"}, scopes...)})
			seen[fam] = true
		}
	}
	var rest []string
	for fam := range families {
		if !seen[fam] {
			rest = append(rest, fam)
		}
	}
	sort.Strings(rest)
	for _, fam := range rest {
		out = append(out, scopeGroup{Family: fam, Scopes: families[fam]})
	}
	return out
}

func (s *Server) handleAdminsPage(w http.ResponseWriter, r *http.Request) {
	d := s.adminPageData(r, r.URL.Query().Get("edit"))
	_ = s.render(w, r, "admins", "app", d)
}

func (s *Server) adminPageData(r *http.Request, edit string) adminsData {
	list, err := s.Admins.List(r.Context())
	d := adminsData{Known: err == nil, Admins: list, ScopeSet: scopeGroups(), Form: operationalForm{Values: map[string]string{"username": "", "role": "admin"}, Fields: map[string]string{}}}
	for i := range list {
		if list[i].ID == edit {
			d.Editing = &list[i]
			d.Selected = list[i].Permissions
		}
	}
	if err != nil {
		s.logError(r, "admin list unavailable", nil)
	}
	return d
}

func (s *Server) adminFormFailure(w http.ResponseWriter, r *http.Request, edit, field string, err error) {
	d := s.adminPageData(r, edit)
	d.Form = submittedOperationalForm(r, d.Form.Values)
	if field != "password" {
		d.Selected = r.Form["permissions"]
	}
	if field != "" {
		d.Form.Fields[field] = "common.error_validation"
	}
	if domain.CodeOf(err) == domain.CodeAdminExists {
		d.Form.Fields["username"] = "admins.error.exists"
	}
	if domain.CodeOf(err) == domain.CodeInvalidRequest {
		for _, pair := range [][2]string{{"username", "username"}, {"password", "password"}, {"permissions", "permissions"}, {"invalid role", "role"}} {
			if strings.HasPrefix(err.Error(), pair[0]) {
				d.Form.Fields[pair[1]] = "common.error_validation"
			}
		}
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	if domain.CodeOf(err) == domain.CodeAdminExists {
		d.Form.Error = s.t(r, "admins.error.exists")
	}
	if domain.CodeOf(err) == domain.CodeOwnerProtected {
		d.Form.Error = s.t(r, "ops.owner_protected")
	}
	_ = s.render(w, r, "admins", "app", d)
}

// handleAdminCreate adds an administrator with the selected permission set
// (owners implicitly hold everything, so the picker only matters for admins).
func (s *Server) handleAdminCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	role := r.PostFormValue("role")
	if role != string(auth.RoleAdmin) && role != string(auth.RoleOwner) {
		role = string(auth.RoleAdmin)
	}
	perms := r.Form["permissions"]
	if role == string(auth.RoleOwner) {
		perms = nil // a scope selection would mislead for owners
	}

	created, err := s.Admins.Create(r.Context(), username, password, auth.Role(role), perms)
	if err != nil {
		s.adminFormFailure(w, r, "", "", err)
		return
	}
	s.audit(r, "admins.created", created.Username, map[string]any{"role": role})
	s.redirectToast(w, r, "/admins", "admins.toast.created")
}

// handleAdminPassword resets one administrator's password (the service
// revokes that account's sessions itself).
func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Admins.SetPassword(r.Context(), id, r.PostFormValue("password")); err != nil {
		s.adminFormFailure(w, r, id, "password", err)
		return
	}
	s.audit(r, "admins.password_reset", id, nil)
	s.redirectToast(w, r, "/admins", "admins.toast.password")
}

// handleAdminPermissions replaces the permission set.
func (s *Server) handleAdminPermissions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	perms := r.Form["permissions"]
	if err := s.Admins.SetPermissions(r.Context(), id, perms); err != nil {
		s.adminFormFailure(w, r, id, "permissions", err)
		return
	}
	s.audit(r, "admins.permissions_updated", id, map[string]any{"count": len(perms)})
	s.redirectToast(w, r, "/admins", "admins.toast.permissions")
}

// handleAdminEnable flips the enabled flag (disabled admins cannot sign in).
func (s *Server) handleAdminEnable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	enable := r.PostFormValue("enable") == "1"
	if err := s.Admins.SetEnabled(r.Context(), id, enable); err != nil {
		s.opsError(w, r, "/admins", err)
		return
	}
	if enable {
		s.redirectToast(w, r, "/admins", "admins.toast.enabled")
	} else {
		s.redirectToast(w, r, "/admins", "admins.toast.disabled")
	}
}

// handleAdminDelete removes one administrator.
func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Admins.Delete(r.Context(), id); err != nil {
		s.opsError(w, r, "/admins", err)
		return
	}
	s.audit(r, "admins.deleted", id, nil)
	s.redirectToast(w, r, "/admins", "admins.toast.deleted")
}

// opsError maps known service errors onto a redisplay; unexpected ones log
// and toast generically.
func (s *Server) opsError(w http.ResponseWriter, r *http.Request, back string, err error) {
	switch domain.CodeOf(err) {
	case domain.CodeOwnerProtected:
		s.redirectToast(w, r, back, "ops.owner_protected")
	case domain.CodeInvalidRequest:
		s.redirectToast(w, r, back, "common.error_validation")
	case domain.CodeNotFound, domain.CodeAdminNotFound:
		s.redirectToast(w, r, back, "common.error_not_found")
	case domain.CodeAdminExists:
		s.redirectToast(w, r, back, "admins.error.exists")
	default:
		s.logError(r, "ops operation failed", nil)
		s.redirectToast(w, r, back, "common.error_generic")
	}
}

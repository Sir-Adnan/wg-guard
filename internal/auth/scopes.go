// Package auth provides the centralized permission registry, argon2id
// password hashing, and admin session management. Authorization is checked
// server-side per handler against this registry — the UI never hides what
// the server doesn't enforce (docs/operations/security.md).
package auth

import (
	"fmt"
	"sort"
	"strings"
)

// Scope constants are the canonical permission strings for this release's
// REST and panel authorization contract (docs/architecture/api.md).
const (
	ScopeUsersRead           = "users.read"
	ScopeUsersCreate         = "users.create"
	ScopeUsersUpdate         = "users.update"
	ScopeUsersDelete         = "users.delete"
	ScopeUsersBulk           = "users.bulk"
	ScopeDevicesRead         = "devices.read"
	ScopeDevicesWrite        = "devices.write"
	ScopeConfigsRead         = "configs.read"
	ScopeTrafficRead         = "traffic.read"
	ScopeTrafficUpdate       = "traffic.update"
	ScopeTemplatesRead       = "templates.read"
	ScopeTemplatesWrite      = "templates.write"
	ScopeStatsRead           = "stats.read"
	ScopeNodeRead            = "node.read"
	ScopeNodeSettings        = "node.settings"
	ScopeWebhooksRead        = "webhooks.read"
	ScopeWebhooksWrite       = "webhooks.write"
	ScopeIfaceRead           = "interfaces.read"
	ScopeIfaceWrite          = "interfaces.write"
	ScopePurchasesCreate     = "purchases.create"
	ScopeOperationsRead      = "operations.read"
	ScopeSubscriptionsRead   = "subscriptions.read"
	ScopeSubscriptionsRotate = "subscriptions.rotate"
	ScopeNextPlansRead       = "next_plans.read"
	ScopeNextPlansWrite      = "next_plans.write"

	// Panel/CLI-only scopes (not part of the token REST surface).
	ScopeAuditView       = "audit.view"
	ScopeAPITokensManage = "api_tokens.manage"
	ScopeAdminsManage    = "admins.manage"
	ScopeServerView      = "server.view"
	ScopeServerManage    = "server.manage"
	ScopeBackupManage    = "backup.manage"
	ScopeUpdateManage    = "update.manage"
	ScopeUpdateRead      = "update.read"
	ScopeCleanupManage   = "cleanup.manage"
)

// scopes is the complete registry. A grant must be a member (or a family
// wildcard, below).
var scopes = map[string]bool{
	ScopeUsersRead: true, ScopeUsersCreate: true, ScopeUsersUpdate: true,
	ScopeUsersDelete: true, ScopeUsersBulk: true,
	ScopeDevicesRead: true, ScopeDevicesWrite: true,
	ScopeConfigsRead: true,
	ScopeTrafficRead: true, ScopeTrafficUpdate: true,
	ScopeTemplatesRead: true, ScopeTemplatesWrite: true,
	ScopeStatsRead: true,
	ScopeNodeRead:  true, ScopeNodeSettings: true,
	ScopeWebhooksRead: true, ScopeWebhooksWrite: true,
	ScopeIfaceRead: true, ScopeIfaceWrite: true,
	ScopePurchasesCreate: true, ScopeOperationsRead: true,
	ScopeSubscriptionsRead: true, ScopeSubscriptionsRotate: true,
	ScopeNextPlansRead: true, ScopeNextPlansWrite: true,
	ScopeAuditView: true, ScopeAPITokensManage: true, ScopeAdminsManage: true,
	ScopeServerView: true, ScopeServerManage: true, ScopeBackupManage: true,
	ScopeUpdateManage: true, ScopeUpdateRead: true, ScopeCleanupManage: true,
}

// New permissions must be classified before they appear in the REST token
// editor. Keeping this allowlist separate from panel grants prevents an
// unrelated panel permission from being offered as a bot credential.
var apiScopes = map[string]bool{
	ScopeUsersRead: true, ScopeUsersCreate: true, ScopeUsersUpdate: true,
	ScopeUsersDelete: true, ScopeUsersBulk: true,
	ScopeDevicesRead: true, ScopeDevicesWrite: true, ScopeConfigsRead: true,
	ScopeTrafficRead: true, ScopeTrafficUpdate: true,
	ScopeTemplatesRead: true, ScopeTemplatesWrite: true, ScopeStatsRead: true,
	ScopeNodeRead: true, ScopeNodeSettings: true,
	ScopeWebhooksRead: true, ScopeWebhooksWrite: true,
	ScopeIfaceRead: true, ScopeIfaceWrite: true,
	ScopePurchasesCreate: true, ScopeOperationsRead: true,
	ScopeSubscriptionsRead: true, ScopeSubscriptionsRotate: true,
	ScopeNextPlansRead: true, ScopeNextPlansWrite: true,
}

var panelOnlyScopes = map[string]bool{
	ScopeAuditView: true, ScopeAPITokensManage: true, ScopeAdminsManage: true,
	ScopeServerView: true, ScopeServerManage: true, ScopeBackupManage: true,
	ScopeUpdateManage: true, ScopeUpdateRead: true, ScopeCleanupManage: true,
}

// resellerGrantable is a closed allowlist: a newly registered node-level
// scope never becomes available to existing reseller accounts implicitly.
var resellerGrantable = map[string]bool{
	ScopeUsersRead: true, ScopeUsersCreate: true,
	ScopeUsersUpdate: true, ScopeUsersDelete: true,
	ScopeUsersBulk: true, ScopeDevicesRead: true,
	ScopeDevicesWrite: true, ScopeConfigsRead: true,
	ScopeTrafficRead: true, ScopeTrafficUpdate: true,
	ScopeTemplatesRead: true, ScopeIfaceRead: true,
	ScopeStatsRead: true, ScopeWebhooksRead: true,
	ScopeWebhooksWrite: true, ScopeAPITokensManage: true,
	ScopePurchasesCreate: true, ScopeOperationsRead: true,
	ScopeSubscriptionsRead: true, ScopeSubscriptionsRotate: true,
	ScopeNextPlansRead: true, ScopeNextPlansWrite: true,
}

func ResellerGrantable(scope string) bool { return resellerGrantable[scope] }

func ValidResellerGrants(scopes []string) bool {
	for _, scope := range scopes {
		if !ResellerGrantable(scope) {
			return false
		}
	}
	return true
}

func ResellerScopes() []string {
	out := make([]string, 0, len(resellerGrantable))
	for scope := range resellerGrantable {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

// AllScopes returns every registered scope, sorted (OpenAPI + UI pickers).
func AllScopes() []string {
	out := make([]string, 0, len(scopes))
	for s := range scopes {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// APIScopes is the REST-token permission surface. Panel-only grants have no
// effect on API requests and are deliberately absent from the token editor.
func APIScopes() []string {
	all := AllScopes()
	out := make([]string, 0, len(all))
	for _, scope := range all {
		if apiScopes[scope] {
			out = append(out, scope)
		}
	}
	return out
}

// ValidateScopes checks a grant list; unknown scopes are rejected so typos
// cannot silently widen access.
func ValidateScopes(granted []string) error {
	for _, g := range granted {
		if g == "*" {
			return fmt.Errorf("wildcard '*' grants are not allowed")
		}
		if strings.HasSuffix(g, ".*") {
			family := strings.TrimSuffix(g, ".*")
			if !familyExists(family) {
				return fmt.Errorf("unknown scope family %q", g)
			}
			continue
		}
		if !scopes[g] {
			return fmt.Errorf("unknown scope %q", g)
		}
	}
	return nil
}

func familyExists(family string) bool {
	prefix := family + "."
	for s := range scopes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// Allows reports whether the granted list satisfies the required scope:
// exact match or a family wildcard (`devices.*` covers `devices.read`).
// Owner role bypasses this check at the caller (role-based, not scope-based).
func Allows(granted []string, required string) bool {
	if !scopes[required] {
		return false // never grant an unregistered requirement
	}
	for _, g := range granted {
		if g == required {
			return true
		}
		if strings.HasSuffix(g, ".*") && strings.HasPrefix(required, strings.TrimSuffix(g, "*")) {
			return true
		}
	}
	return false
}

// AllowsAll is the conjunctive form used by handlers needing two scopes.
func AllowsAll(granted []string, required ...string) bool {
	for _, r := range required {
		if !Allows(granted, r) {
			return false
		}
	}
	return true
}

// Role is an admin role. Owner is immutable and implicitly authorized for
// everything; the Owner cannot remove or demote itself (requirements.md).
type Role string

const (
	RoleOwner Role = "owner"
	RoleAdmin Role = "admin"
)

func (r Role) Valid() bool { return r == RoleOwner || r == RoleAdmin }

// Authorized decides for an admin account: owners pass everything; admins
// need the scope granted in their permissions list.
func Authorized(role Role, permissions []string, required string) bool {
	if role == RoleOwner {
		return true
	}
	return Allows(permissions, required)
}

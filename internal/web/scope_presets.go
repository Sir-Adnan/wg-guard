package web

import "github.com/Sir-Adnan/wg-guard/internal/auth"

// Permission shortcuts are a presentation policy, not an authorization rule.
// Every new scope needs an explicit classification; an unclassified scope is
// never selected by a shortcut. Family wildcards are never selected either,
// because they would implicitly grant future scopes in that family.
var scopePresetTiers = map[string]string{
	auth.ScopeUsersRead: "observer", auth.ScopeUsersCreate: "operator",
	auth.ScopeUsersUpdate: "operator", auth.ScopeUsersDelete: "full",
	auth.ScopeUsersBulk:   "full", // bulk-action includes deletion
	auth.ScopeDevicesRead: "observer", auth.ScopeDevicesWrite: "operator",
	auth.ScopeConfigsRead: "operator", // exposes private client material
	auth.ScopeTrafficRead: "observer", auth.ScopeTrafficUpdate: "operator",
	auth.ScopeTemplatesRead: "observer", auth.ScopeTemplatesWrite: "full",
	auth.ScopeStatsRead: "observer", auth.ScopeNodeRead: "observer",
	auth.ScopeNodeSettings: "full", auth.ScopeWebhooksRead: "full",
	auth.ScopeWebhooksWrite: "full", auth.ScopeIfaceRead: "observer",
	auth.ScopeIfaceWrite: "full", auth.ScopePurchasesCreate: "operator",
	auth.ScopeOperationsRead: "observer", auth.ScopeSubscriptionsRead: "operator",
	auth.ScopeSubscriptionsRotate: "full", auth.ScopeNextPlansRead: "observer",
	auth.ScopeNextPlansWrite: "operator", auth.ScopeAuditView: "full",
	auth.ScopeAPITokensManage: "full", auth.ScopeAdminsManage: "full",
	auth.ScopeServerView: "full", auth.ScopeServerManage: "full",
	auth.ScopeBackupManage: "full", auth.ScopeUpdateManage: "full",
	auth.ScopeUpdateRead: "observer",
}

func (v *View) ScopePresetTier(scope string) string { return scopePresetTiers[scope] }

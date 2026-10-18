package web

import "fmt"

// navItem is one destination in the sidebar and the command palette.
type navItem struct {
	Href, Label, Icon string
	Active            bool
	Marker            string // optional accessible label for an attention dot
}

type navGroup struct {
	Label string
	Items []navItem
}

// Nav returns the permission-aware navigation. The server still authorizes
// every route; absent entries are a courtesy, not the security boundary.
// The sidebar and the command palette both render from this one source.
func (v *View) Nav() []navGroup {
	if v.Admin == nil {
		return nil
	}
	item := func(href, key, icon string, active bool) navItem {
		return navItem{Href: href, Label: v.T(key), Icon: icon, Active: active}
	}
	var groups []navGroup
	add := func(key string, items ...navItem) {
		if len(items) > 0 {
			groups = append(groups, navGroup{Label: v.T(key), Items: items})
		}
	}
	if v.Admin.ResellerID != nil {
		var manage, system []navItem
		if v.Can("users.read") {
			manage = append(manage, item("/reseller/users", "nav.users", "users", v.IsActive("/reseller/users")))
		}
		if v.Can("api_tokens.manage") {
			system = append(system, item("/reseller/tokens", "nav.tokens", "key", v.IsActive("/reseller/tokens")))
		}
		if v.Can("webhooks.read") {
			system = append(system, item("/reseller/webhooks", "nav.webhooks", "link", v.IsActive("/reseller/webhooks")))
		}
		system = append(system, item("/appearance", "appearance.title", "sparkles", v.IsActive("/appearance")))
		add("nav.group.manage", manage...)
		add("nav.group.system", system...)
		return groups
	}
	var manage, system, prefs []navItem
	if v.Can("stats.read") {
		manage = append(manage, item("/dashboard", "nav.dashboard", "layout-dashboard", v.Path == "/dashboard" || v.Path == "/"))
	}
	if v.Can("users.read") || v.Can("users.create") || v.Can("users.bulk") {
		href := "/users"
		if !v.Can("users.read") {
			href = "/users/bulk"
			if v.Can("users.create") {
				href = "/users/new"
			}
		}
		manage = append(manage, item(href, "nav.users", "users", v.IsActive("/users")))
	}
	if v.Can("templates.read") || v.Can("templates.write") {
		href := "/templates"
		if !v.Can("templates.read") {
			href = "/templates/new"
		}
		manage = append(manage, item(href, "nav.plans", "layers", v.IsActive("/templates")))
	}
	if v.Can("interfaces.read") || v.Can("interfaces.write") {
		href := "/interfaces"
		if !v.Can("interfaces.read") {
			href = "/interfaces/new"
		}
		manage = append(manage, item(href, "nav.interfaces", "network", v.IsActive("/interfaces")))
	}
	if v.Can("interfaces.read") {
		manage = append(manage, item("/routing/outbounds", "nav.routing", "route", v.IsActive("/routing")))
	}
	if v.Can("backup.manage") {
		system = append(system, item("/backups", "nav.backups", "archive", v.IsActive("/backups")))
	}
	if v.Can("update.manage") || v.Can("update.read") {
		updates := item("/updates", "nav.updates", "refresh-cw", v.IsActive("/updates"))
		if v.UpdateNotice {
			updates.Marker = v.T("updates.available")
		}
		system = append(system, updates)
	}
	if v.Can("node.read") {
		system = append(system, item("/system", "system.title", "activity", v.IsActive("/system")))
	}
	if v.Can("admins.manage") {
		system = append(system, item("/admins", "nav.admins", "user-plus", v.IsActive("/admins")))
	}
	if fmt.Sprint(v.Admin.Role) == "owner" {
		system = append(system, item("/resellers", "resellers.title", "users", v.IsActive("/resellers")))
	}
	if v.Can("api_tokens.manage") {
		system = append(system, item("/tokens", "nav.tokens", "key", v.IsActive("/tokens")))
	}
	if v.Can("webhooks.read") {
		system = append(system, item("/webhooks", "nav.webhooks", "link", v.IsActive("/webhooks")))
	}
	if v.Can("audit.view") {
		system = append(system, item("/audit", "nav.audit", "inbox", v.IsActive("/audit")))
	}
	if v.Can("node.settings") {
		prefs = append(prefs, item("/settings", "nav.settings", "settings-2", v.Path == "/settings"))
	}
	if v.Can("server.view") {
		prefs = append(prefs, item("/settings/domains", "domains.title", "globe", v.IsActive("/settings/domains")))
	}
	if v.Can("cleanup.manage") {
		prefs = append(prefs, item("/cleanup", "cleanup.title", "database", v.IsActive("/cleanup")))
	}
	prefs = append(prefs, item("/appearance", "appearance.title", "sparkles", v.IsActive("/appearance")))
	add("nav.group.manage", manage...)
	add("nav.group.system", system...)
	add("nav.group.preferences", prefs...)
	return groups
}

// HomeHref is the brand link target for the signed-in account.
func (v *View) HomeHref() string {
	if v.Admin != nil && v.Admin.ResellerID != nil {
		return "/reseller/users"
	}
	return "/"
}

// RoleLabel is the localized role of the signed-in account.
func (v *View) RoleLabel() string {
	if v.Admin == nil {
		return ""
	}
	switch role := fmt.Sprint(v.Admin.Role); role {
	case "owner":
		return v.T("role.owner")
	case "admin":
		return v.T("role.admin")
	default:
		return role
	}
}

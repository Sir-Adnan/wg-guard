package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
)

// Settings presentation metadata belongs to the web layer; validation and
// persistence remain exclusively in the registry.
type settingsData struct {
	Error, Field, ErrorLabel string
	TLSMode, ToolsVer        string
	LoadFailed               bool
	Sections                 []settingsSection
}

type settingsSection struct {
	ID, Title, Description string
	Fields, Advanced       []settingsControl
	AdvancedOpen           bool
}

type settingsOption struct {
	Value, Label string
	Selected     bool
}
type settingsControl struct {
	Unit                                                    string
	Name, Label, Hint, Effect, Value, Default, Range        string
	Numeric, Secret, SecretSet, Clear, Unavailable, Invalid bool
	Options                                                 []settingsOption
}

type settingsPresentation struct {
	name, label, hint, effect string
	advanced                  bool
}

type settingsGroup struct {
	id, title string
	fields    []settingsPresentation
}

// The seven product groups deliberately differ from registry storage categories.
var settingsGroups = []settingsGroup{
	{"identity", "settings.section_identity", []settingsPresentation{
		{"node_id", "settings.node_id", "settings.help.node", "settings.effect.node", false},
		{"endpoint", "settings.endpoint", "settings.help.endpoint", "settings.effect.config", false},
	}},
	{"users", "settings.section_users", []settingsPresentation{
		{"default_quota_gb", "settings.default_quota", "settings.default_quota_hint", "settings.effect.users", false},
		{"default_dur_months", "settings.default_dur", "settings.default_dur_hint", "settings.effect.users", false},
		{"default_device_lim", "settings.default_device", "settings.help.devices", "settings.effect.users", false},
		{"default_iface_id", "settings.default_iface", "settings.help.interface", "settings.effect.users", false},
		{"quota_presets", "settings.quota_presets", "settings.quota_presets_hint", "settings.effect.users", true},
		{"dur_presets", "settings.dur_presets", "settings.dur_presets_hint", "settings.effect.users", true},
	}},
	{"network", "settings.section_networking", []settingsPresentation{
		{"dns_servers", "settings.dns", "settings.dns_hint", "settings.effect.config", false},
		{"allowed_ips", "settings.allowed_ips", "settings.allowed_ips_hint", "settings.effect.config", false},
		{"mtu", "settings.mtu", "settings.help.mtu", "settings.effect.interfaces", false},
		{"keepalive", "settings.keepalive", "settings.keepalive_hint", "settings.effect.config", false},
		{"port_min", "settings.port_min", "settings.help.port", "settings.effect.interfaces", true},
		{"port_max", "settings.port_max", "settings.help.port", "settings.effect.interfaces", true},
		{"default_pool", "settings.default_pool", "settings.default_pool_hint", "settings.effect.interfaces", true},
		{"iface_max", "settings.iface_max", "settings.help.cap", "settings.effect.interfaces", true},
		{"drift_policy", "settings.drift_policy", "settings.help.drift", "settings.effect.restart", true},
	}},
	{"subscription", "settings.group.subscription", []settingsPresentation{
		{"sub_base_url", "settings.sub_base_url", "settings.sub_base_url_hint", "settings.effect.links", false},
		{"filename_prefix", "settings.filename_prefix", "settings.filename_hint", "settings.effect.downloads", false},
		{"filename_suffix", "settings.filename_suffix", "settings.help.filename", "settings.effect.downloads", false},
	}},
	{"accounting", "settings.section_accounting", []settingsPresentation{
		{"acct_interval", "settings.acct_interval", "settings.help.cycle", "settings.effect.accounting", false},
		{"online_window", "settings.online_window", "settings.help.online", "settings.effect.online", false},
		{"sample_flush", "settings.sample_flush", "settings.help.flush", "settings.effect.flush", true},
		{"sample_retention", "settings.sample_retention", "settings.help.raw", "settings.effect.prune", true},
		{"rollup_hourly", "settings.rollup_hourly", "settings.help.hourly", "settings.effect.prune", true},
		{"rollup_daily", "settings.rollup_daily", "settings.help.daily", "settings.effect.prune", true},
	}},
	{"access", "settings.section_api", []settingsPresentation{
		{"session_idle", "settings.session_idle", "settings.help.idle", "settings.effect.restart", false},
		{"session_abs", "settings.session_abs", "settings.help.absolute", "settings.effect.restart", false},
		{"rate_limit", "settings.rate_limit", "settings.rate_limit_hint", "settings.effect.api", true},
		{"webhook_max", "settings.webhook_max", "settings.webhook_max_hint", "settings.effect.webhook", true},
	}},
	{"backup", "settings.section_backup", []settingsPresentation{
		{"retention", "settings.retention", "settings.retention_hint", "settings.effect.backup", false},
		{"backup_password", "settings.backup_password", "settings.backup_password_hint", "settings.effect.password", false},
		{"telegram_chat", "settings.telegram_chat", "settings.telegram_chat_hint", "settings.effect.telegram", true},
		{"telegram_token", "settings.telegram_token", "settings.telegram_token_hint", "settings.effect.telegram", true},
	}},
}

func settingsValue(v any) string {
	if list, ok := v.([]string); ok {
		return strings.Join(list, ", ")
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	d := s.loadSettingsData(r)
	_ = s.render(w, r, "settings", "app", d)
}

func (s *Server) loadSettingsData(r *http.Request) settingsData {
	d := settingsData{TLSMode: string(s.TLSMode), ToolsVer: s.ToolsVersion}
	defs := map[string]settings.Definition{}
	for _, def := range s.Settings.Definitions() {
		defs[def.Key] = def
	}
	specs := map[string]setSpec{}
	for _, spec := range settingSpecs {
		specs[spec.form] = spec
	}
	specs["backup_password"] = setSpec{"backup.password", "backup_password", "secret"}
	specs["telegram_token"] = setSpec{"backup.telegram_token", "telegram_token", "secret"}
	ifaces, ifaceErr := s.Ifaces.List(r.Context())
	for _, group := range settingsGroups {
		section := settingsSection{ID: group.id, Title: group.title, Description: "settings.intro." + group.id}
		for _, p := range group.fields {
			spec := specs[p.name]
			def, found := defs[spec.key]
			f := settingsControl{Name: p.name, Label: p.label, Hint: p.hint, Effect: p.effect, Numeric: spec.kind == "int", Secret: spec.kind == "secret", Default: settingsValue(def.Default)}
			switch f.Name {
			case "default_quota_gb":
				f.Unit = "settings.unit.gb"
			case "default_dur_months":
				f.Unit = "duration.months"
			case "acct_interval", "online_window", "sample_flush":
				f.Unit = "duration.seconds_short"
			case "sample_retention", "session_idle", "session_abs":
				f.Unit = "duration.hours"
			case "rollup_hourly", "rollup_daily":
				f.Unit = "duration.days"
			}
			if f.Numeric {
				f.Range = fmt.Sprintf("%d–%d", def.Min, def.Max)
			}
			var err error
			if f.Secret {
				var secret string
				secret, err = s.Settings.GetSecret(r.Context(), spec.key)
				f.SecretSet = err == nil && secret != ""
			} else {
				var value any
				value, err = s.Settings.Get(r.Context(), spec.key)
				if err == nil {
					f.Value = settingsValue(value)
				}
			}
			f.Unavailable = err != nil || !found
			if f.Unavailable {
				d.LoadFailed = true
			}
			if r.Method == http.MethodPost {
				if !f.Secret && r.Form.Has(f.Name) {
					f.Value = r.PostFormValue(f.Name)
				}
				if f.Secret {
					f.Clear = r.PostFormValue(f.Name+"_clear") == "1"
				}
			}
			if p.name == "default_iface_id" {
				f.Options = append(f.Options, settingsOption{"", s.t(r, "settings.default_iface_auto"), f.Value == ""})
				selectedFound := f.Value == ""
				for _, iface := range ifaces {
					label := iface.Name
					if !iface.Enabled {
						label += " · " + s.t(r, "settings.interface_disabled")
					}
					f.Options = append(f.Options, settingsOption{iface.ID, label, f.Value == iface.ID})
					selectedFound = selectedFound || f.Value == iface.ID
				}
				if !selectedFound {
					f.Options = append(f.Options, settingsOption{f.Value, s.t(r, "settings.selection_unavailable") + " · " + f.Value, true})
				}
				if ifaceErr != nil {
					f.Unavailable = true
					d.LoadFailed = true
				}
			} else if len(def.Options) > 0 {
				selectedFound := false
				for _, option := range def.Options {
					f.Options = append(f.Options, settingsOption{option, s.t(r, "settings.drift."+option), f.Value == option})
					selectedFound = selectedFound || f.Value == option
				}
				if !selectedFound {
					f.Options = append(f.Options, settingsOption{f.Value, f.Value, true})
				}
			}
			if p.advanced {
				section.Advanced = append(section.Advanced, f)
			} else {
				section.Fields = append(section.Fields, f)
			}
		}
		d.Sections = append(d.Sections, section)
	}
	return d
}

// setSpec is one saveable field: registry key, form field name, kind.
type setSpec struct {
	key, form, kind string // kind: str | int | list
}

var settingSpecs = []setSpec{
	{"node.id", "node_id", "str"},
	{"node.endpoint", "endpoint", "str"},
	{"users.quota_presets_gb", "quota_presets", "list"},
	{"users.duration_presets_months", "dur_presets", "list"},
	{"users.default_quota_gb", "default_quota_gb", "int"},
	{"users.default_duration_months", "default_dur_months", "int"},
	{"users.default_device_limit", "default_device_lim", "int"},
	{"users.default_iface_id", "default_iface_id", "str"},
	{"subscription.base_url", "sub_base_url", "str"},
	{"downloads.filename_prefix", "filename_prefix", "str"},
	{"downloads.filename_suffix", "filename_suffix", "str"},
	{"network.mtu", "mtu", "int"},
	{"network.dns_servers", "dns_servers", "list"},
	{"network.client_allowed_ips", "allowed_ips", "str"},
	{"network.client_persistent_keepalive", "keepalive", "str"},
	{"network.port_min", "port_min", "int"},
	{"network.port_max", "port_max", "int"},
	{"network.default_pool", "default_pool", "str"},
	{"interfaces.max_count", "iface_max", "int"},
	{"drift.policy", "drift_policy", "str"},
	{"accounting.interval_seconds", "acct_interval", "int"},
	{"accounting.online_window_seconds", "online_window", "int"},
	{"accounting.sample_flush_seconds", "sample_flush", "int"},
	{"accounting.sample_retention_hours", "sample_retention", "int"},
	{"accounting.rollup_hourly_days", "rollup_hourly", "int"},
	{"accounting.rollup_daily_days", "rollup_daily", "int"},
	{"api.rate_limit_per_minute", "rate_limit", "int"},
	{"webhooks.max_attempts", "webhook_max", "int"},
	{"security.session_idle_hours", "session_idle", "int"},
	{"security.session_absolute_hours", "session_abs", "int"},
	{"backup.retention_count", "retention", "int"},
	{"backup.telegram_chat", "telegram_chat", "str"},
}

// handleSettingsSave applies the settings form through the registry
// validators, then the backup secrets (write-only: an empty field keeps the
// stored value; the matching clear checkbox removes it; a value replaces and
// the registry validator enforces strength).
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	type pendingSetting struct {
		field  string
		update settings.Update
	}
	pending := make([]pendingSetting, 0, len(settingSpecs)+2)
	for _, spec := range settingSpecs {
		if !r.Form.Has(spec.form) {
			continue // absent fields keep their stored value
		}
		var value any
		switch spec.kind {
		case "int":
			raw := strings.TrimSpace(r.PostFormValue(spec.form))
			if raw == "" {
				continue // empty integer inputs keep the stored value
			}
			n, err := strconv.Atoi(raw)
			if err != nil {
				s.settingsSaveError(w, r, spec.form, domain.E(domain.CodeInvalidRequest, "not a number"))
				return
			}
			value = n
		case "list":
			parts := strings.Split(r.PostFormValue(spec.form), ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			value = out
		default:
			value = strings.TrimSpace(r.PostFormValue(spec.form))
		}
		pending = append(pending, pendingSetting{field: spec.form, update: settings.Update{Key: spec.key, Value: value}})
	}

	for _, secret := range []struct{ key, field, clear string }{
		{"backup.password", "backup_password", "backup_password_clear"},
		{"backup.telegram_token", "telegram_token", "telegram_token_clear"},
	} {
		value := strings.TrimSpace(r.PostFormValue(secret.field))
		if r.PostFormValue(secret.clear) == "1" {
			value = ""
		} else if value == "" {
			continue // keep the stored value
		}
		pending = append(pending, pendingSetting{field: secret.field, update: settings.Update{Key: secret.key, Value: value}})
	}

	updates := make([]settings.Update, 0, len(pending))
	for _, item := range pending {
		if err := s.Settings.Validate(item.update.Key, item.update.Value); err != nil {
			s.settingsSaveError(w, r, item.field, err)
			return
		}
		updates = append(updates, item.update)
	}
	if err := s.Settings.SetBatch(r.Context(), updates); err != nil {
		s.settingsSaveError(w, r, "", err)
		return
	}

	s.audit(r, "settings.updated", "", nil)
	s.redirectToast(w, r, "/settings", "settings.toast.saved")
}

// settingsSaveError re-renders the form with the submitted values and a
// humanized error banner; the offending field gets the invalid marker.
func (s *Server) settingsSaveError(w http.ResponseWriter, r *http.Request, field string, err error) {
	switch domain.CodeOf(err) {
	case domain.CodeInvalidRequest, domain.CodeSettingUnknown, domain.CodeSettingInvalid:
		d := s.submittedSettingsData(r)
		d.markInvalid(field)
		d.Error = s.humanizeDomainError(r, err)
		_ = s.render(w, r, "settings", "app", d)
		return
	}
	s.logError(r, "settings save", err)
	d := s.submittedSettingsData(r)
	d.Error = s.t(r, "common.error_generic")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusInternalServerError)
	_ = s.render(w, r, "settings", "app", d)
}

// submittedSettingsData preserves exact nonsensitive POST values, including
// explicit secret-clear intent; plaintext secrets are never returned to the UI.
func (s *Server) submittedSettingsData(r *http.Request) settingsData { return s.loadSettingsData(r) }

func (d *settingsData) markInvalid(name string) {
	d.Field = name
	for i := range d.Sections {
		section := &d.Sections[i]
		for j := range section.Fields {
			if section.Fields[j].Name == name {
				section.Fields[j].Invalid = true
				d.ErrorLabel = section.Fields[j].Label
			}
		}
		for j := range section.Advanced {
			if section.Advanced[j].Name == name {
				section.Advanced[j].Invalid = true
				section.AdvancedOpen = true
				d.ErrorLabel = section.Advanced[j].Label
			}
		}
	}
}

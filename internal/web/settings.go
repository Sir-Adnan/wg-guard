package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// settingsData feeds the panel settings screen: the runtime registry. Non-
// secret knobs read and write; backup secrets are write-only (status badge +
// replace/clear). Read and write are gated by node.settings.
type settingsData struct {
	Error string // humanized message
	Field string // field that failed (best effort)

	// Identity
	NodeID   string
	NodeSet  bool
	Endpoint string
	TLSMode  string
	ToolsVer string

	// Users
	QuotaPresets     string
	DurPresets       string
	DefaultQuotaGB   int
	DefaultDurMonths int
	DefaultDeviceLim int
	DefaultIfaceID   string

	// Subscription + downloads
	SubBaseURL     string
	FilenamePrefix string
	FilenameSuffix string

	// Networking
	MTU          int
	DNSServers   string
	AllowedIPs   string
	Keepalive    int
	PortMin      int
	PortMax      int
	DefaultPool  string
	IfaceMax     int
	DriftPolicy  string
	DriftOptions []string

	// Accounting
	AcctInterval     int
	OnlineWindow     int
	SampleFlush      int
	SampleRetention  int
	RollupHourlyDays int
	RollupDailyDays  int

	// API + security
	RateLimit   int
	WebhookMax  int
	SessionIdle int
	SessionAbs  int

	// Backups
	Retention    int
	PasswordSet  bool
	TelegramSet  bool
	TelegramChat string

	Ifaces []*ifaceRef
}

// handleSettingsPage renders the settings form from the live registry.
func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	d := s.loadSettingsData(r)
	_ = s.render(w, r, "settings", "app", d)
}

func (s *Server) loadSettingsData(r *http.Request) settingsData {
	ctx := r.Context()
	d := settingsData{Ifaces: s.ifacesForForm(r), DriftOptions: []string{"report", "adopt", "remove"}}
	get := func(key string) string { v, _ := s.Settings.GetString(ctx, key); return v }
	getInt := func(key string) int { v, _ := s.Settings.GetInt(ctx, key); return v }
	getList := func(key string) string {
		v, err := s.Settings.GetStringList(ctx, key)
		if err != nil {
			return ""
		}
		return strings.Join(v, ", ")
	}

	d.NodeID = get("node.id")
	d.NodeSet = d.NodeID != ""
	d.Endpoint = get("node.endpoint")
	d.TLSMode = string(s.TLSMode)
	d.ToolsVer = s.ToolsVersion

	d.QuotaPresets = getList("users.quota_presets_gb")
	d.DurPresets = getList("users.duration_presets_months")
	d.DefaultQuotaGB = getInt("users.default_quota_gb")
	d.DefaultDurMonths = getInt("users.default_duration_months")
	d.DefaultDeviceLim = getInt("users.default_device_limit")
	d.DefaultIfaceID = get("users.default_iface_id")
	d.SubBaseURL = get("subscription.base_url")
	d.FilenamePrefix = get("downloads.filename_prefix")
	d.FilenameSuffix = get("downloads.filename_suffix")

	d.MTU = getInt("network.mtu")
	d.DNSServers = getList("network.dns_servers")
	d.AllowedIPs = get("network.client_allowed_ips")
	d.Keepalive = getInt("network.client_keepalive_seconds")
	d.PortMin = getInt("network.port_min")
	d.PortMax = getInt("network.port_max")
	d.DefaultPool = get("network.default_pool")
	d.IfaceMax = getInt("interfaces.max_count")
	d.DriftPolicy = get("drift.policy")

	d.AcctInterval = getInt("accounting.interval_seconds")
	d.OnlineWindow = getInt("accounting.online_window_seconds")
	d.SampleFlush = getInt("accounting.sample_flush_seconds")
	d.SampleRetention = getInt("accounting.sample_retention_hours")
	d.RollupHourlyDays = getInt("accounting.rollup_hourly_days")
	d.RollupDailyDays = getInt("accounting.rollup_daily_days")

	d.RateLimit = getInt("api.rate_limit_per_minute")
	d.WebhookMax = getInt("webhooks.max_attempts")
	d.SessionIdle = getInt("security.session_idle_hours")
	d.SessionAbs = getInt("security.session_absolute_hours")

	d.Retention = getInt("backup.retention_count")
	if pw, err := s.Settings.GetSecret(ctx, "backup.password"); err == nil {
		d.PasswordSet = pw != ""
	}
	if tok, err := s.Settings.GetSecret(ctx, "backup.telegram_token"); err == nil {
		d.TelegramSet = tok != ""
	}
	d.TelegramChat = get("backup.telegram_chat")
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
	{"network.client_keepalive_seconds", "keepalive", "int"},
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
	for _, spec := range settingSpecs {
		if !r.Form.Has(spec.form) {
			continue // absent fields keep their stored value
		}
		var err error
		switch spec.kind {
		case "int":
			raw := strings.TrimSpace(r.PostFormValue(spec.form))
			if raw == "" {
				continue // absent inputs keep the stored value
			}
			var n int
			if n, err = strconv.Atoi(raw); err != nil {
				err = domain.E(domain.CodeInvalidRequest, "not a number")
			} else {
				err = s.Settings.Set(r.Context(), spec.key, n)
			}
		case "list":
			parts := strings.Split(r.PostFormValue(spec.form), ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			err = s.Settings.Set(r.Context(), spec.key, out)
		default:
			err = s.Settings.Set(r.Context(), spec.key, strings.TrimSpace(r.PostFormValue(spec.form)))
		}
		if err != nil {
			s.settingsSaveError(w, r, spec.form, err)
			return
		}
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
		if err := s.Settings.Set(r.Context(), secret.key, value); err != nil {
			s.settingsSaveError(w, r, secret.field, err)
			return
		}
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
		d.Field = field
		d.Error = s.humanizeDomainError(r, err)
		_ = s.render(w, r, "settings", "app", d)
		return
	}
	s.logError(r, "settings save", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// submittedSettingsData rebuilds the form from the POST so a failed save
// never silently drops what the operator typed.
func (s *Server) submittedSettingsData(r *http.Request) settingsData {
	d := settingsData{Ifaces: s.ifacesForForm(r)}
	str := func(field string) string { return r.PostFormValue(field) }
	intOf := func(field string) int { n, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue(field))); return n }
	for _, spec := range settingSpecs {
		switch spec.form {
		case "node_id":
			d.NodeID = str(spec.form)
		case "endpoint":
			d.Endpoint = str(spec.form)
		case "quota_presets":
			d.QuotaPresets = str(spec.form)
		case "dur_presets":
			d.DurPresets = str(spec.form)
		case "default_quota_gb":
			d.DefaultQuotaGB = intOf(spec.form)
		case "default_dur_months":
			d.DefaultDurMonths = intOf(spec.form)
		case "default_device_lim":
			d.DefaultDeviceLim = intOf(spec.form)
		case "default_iface_id":
			d.DefaultIfaceID = str(spec.form)
		case "sub_base_url":
			d.SubBaseURL = str(spec.form)
		case "filename_prefix":
			d.FilenamePrefix = str(spec.form)
		case "filename_suffix":
			d.FilenameSuffix = str(spec.form)
		case "mtu":
			d.MTU = intOf(spec.form)
		case "dns_servers":
			d.DNSServers = str(spec.form)
		case "allowed_ips":
			d.AllowedIPs = str(spec.form)
		case "keepalive":
			d.Keepalive = intOf(spec.form)
		case "port_min":
			d.PortMin = intOf(spec.form)
		case "port_max":
			d.PortMax = intOf(spec.form)
		case "default_pool":
			d.DefaultPool = str(spec.form)
		case "iface_max":
			d.IfaceMax = intOf(spec.form)
		case "drift_policy":
			d.DriftPolicy = str(spec.form)
		case "acct_interval":
			d.AcctInterval = intOf(spec.form)
		case "online_window":
			d.OnlineWindow = intOf(spec.form)
		case "sample_flush":
			d.SampleFlush = intOf(spec.form)
		case "sample_retention":
			d.SampleRetention = intOf(spec.form)
		case "rollup_hourly":
			d.RollupHourlyDays = intOf(spec.form)
		case "rollup_daily":
			d.RollupDailyDays = intOf(spec.form)
		case "rate_limit":
			d.RateLimit = intOf(spec.form)
		case "webhook_max":
			d.WebhookMax = intOf(spec.form)
		case "session_idle":
			d.SessionIdle = intOf(spec.form)
		case "session_abs":
			d.SessionAbs = intOf(spec.form)
		case "retention":
			d.Retention = intOf(spec.form)
		case "telegram_chat":
			d.TelegramChat = str(spec.form)
		}
	}
	return d
}

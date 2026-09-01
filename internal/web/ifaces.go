package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/awgparam"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
)

// ifaceRow decorates an interface for the list (device count).
type ifaceRow struct {
	I       *iface.Interface
	Devices int
}

type ifacesData struct {
	Rows []ifaceRow
}

func (s *Server) handleIfaceList(w http.ResponseWriter, r *http.Request) {
	ifaces, err := s.Ifaces.List(r.Context())
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	ids := make([]string, 0, len(ifaces))
	for _, i := range ifaces {
		ids = append(ids, i.ID)
	}
	counts, err := s.Devices.CountForIfaces(r.Context(), ids)
	if err != nil {
		s.logError(r, "iface device counts", err)
	}
	rows := make([]ifaceRow, 0, len(ifaces))
	for _, i := range ifaces {
		rows = append(rows, ifaceRow{I: i, Devices: counts[i.ID]})
	}
	_ = s.render(w, r, "ifaces", "app", ifacesData{Rows: rows})
}

// ifaceFormData backs the new/edit page. I is nil on create.
type ifaceFormData struct {
	I *iface.Interface
}

func (s *Server) handleIfaceNew(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "iface_form", "app", ifaceFormData{})
}

func (s *Server) handleIfaceEditPage(w http.ResponseWriter, r *http.Request) {
	i, err := s.Ifaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	_ = s.render(w, r, "iface_form", "app", ifaceFormData{I: i})
}

func (s *Server) handleProfilePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	policy := iface.ProfilePolicy(strings.TrimSpace(r.PostFormValue("policy")))
	if policy != iface.ProfileRecommended && policy != iface.ProfileRandomized {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_profile_policy"})
		return
	}
	if s.ProfileGenerator == nil {
		s.logError(r, "profile preview generator unavailable", nil)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "profile_generation_failed"})
		return
	}
	profile, err := s.ProfileGenerator(policy)
	if err != nil {
		s.logError(r, "profile preview generation", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "profile_generation_failed"})
		return
	}
	_ = json.NewEncoder(w).Encode(profilePreviewResponse{
		Policy: string(policy),
		Fields: profileFormFields(profile),
	})
}

type profilePreviewResponse struct {
	Policy string            `json:"policy"`
	Fields map[string]string `json:"fields"`
}

func profileFormFields(profile iface.Obfuscation) map[string]string {
	integer := func(value int) string {
		if value == 0 {
			return ""
		}
		return strconv.Itoa(value)
	}
	u16Range := func(value awgparam.U16Range) string {
		if value.IsZero() {
			return ""
		}
		return value.String()
	}
	checked := func(value bool) string {
		if value {
			return "1"
		}
		return ""
	}
	return map[string]string{
		"obf_enabled":         checked(profile.Enabled),
		"obf_jc":              integer(profile.Jc),
		"obf_jmin":            integer(profile.Jmin),
		"obf_jmax":            integer(profile.Jmax),
		"obf_s1":              integer(profile.S1),
		"obf_s2":              integer(profile.S2),
		"obf_s3":              integer(profile.S3),
		"obf_s4":              integer(profile.S4),
		"obf_h1":              profile.H1.String(),
		"obf_h2":              profile.H2.String(),
		"obf_h3":              profile.H3.String(),
		"obf_h4":              profile.H4.String(),
		"obf_i1":              profile.I1,
		"obf_i2":              profile.I2,
		"obf_i3":              profile.I3,
		"obf_i4":              profile.I4,
		"obf_i5":              profile.I5,
		"obf_hpk":             profile.HeaderProtectionKey,
		"obf_padding":         u16Range(profile.ContentPaddingAddition),
		"obf_rekey_after":     u16Range(profile.RekeyAfterTime),
		"obf_rekey_timeout":   u16Range(profile.RekeyTimeout),
		"obf_reject_after":    u16Range(profile.RejectAfterTime),
		"obf_keepalive":       u16Range(profile.KeepaliveTimeout),
		"obf_max_handshake":   u16Range(profile.MaxHandshakeAttempts),
		"obf_random_trailers": checked(profile.RandomTrailers),
		"obf_disable_cookies": checked(profile.DisableCookies),
	}
}

// obfuscationFromForm parses the obfuscation section without coercing invalid
// values to zero. Empty fields map to zero so the enabled toggle stays clean;
// relationship validation remains in the interface service.
func obfuscationFromForm(r *http.Request, existing *iface.Obfuscation) (iface.Obfuscation, error) {
	o := iface.Obfuscation{Enabled: r.PostFormValue("obf_enabled") == "1"}
	atoi := func(key string) (int, error) {
		text := strings.TrimSpace(r.PostFormValue(key))
		if text == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, domain.E(domain.CodeParamConstraint, "%s must be an integer", key)
		}
		return n, nil
	}
	trim := func(key string) string { return strings.TrimSpace(r.PostFormValue(key)) }
	var err error
	for _, field := range []struct {
		key string
		dst *int
	}{
		{"obf_jc", &o.Jc}, {"obf_jmin", &o.Jmin}, {"obf_jmax", &o.Jmax},
		{"obf_s1", &o.S1}, {"obf_s2", &o.S2}, {"obf_s3", &o.S3}, {"obf_s4", &o.S4},
	} {
		if *field.dst, err = atoi(field.key); err != nil {
			return iface.Obfuscation{}, err
		}
	}
	for _, field := range []struct {
		key string
		dst *awgparam.U32Range
	}{
		{"obf_h1", &o.H1}, {"obf_h2", &o.H2}, {"obf_h3", &o.H3}, {"obf_h4", &o.H4},
	} {
		text := trim(field.key)
		if text == "" {
			continue
		}
		if *field.dst, err = awgparam.ParseU32Range(text); err != nil {
			return iface.Obfuscation{}, domain.E(domain.CodeParamConstraint, "%s must be N or low-high within u32 bounds", field.key)
		}
	}
	for _, field := range []struct {
		key string
		dst *string
	}{
		{"obf_i1", &o.I1}, {"obf_i2", &o.I2}, {"obf_i3", &o.I3},
		{"obf_i4", &o.I4}, {"obf_i5", &o.I5},
	} {
		*field.dst = trim(field.key)
	}
	// HeaderProtectionKey is write-only in rendered edit pages. An empty field
	// preserves the stored key; the explicit clear checkbox removes it.
	if o.Enabled {
		o.HeaderProtectionKey = trim("obf_hpk")
		if o.HeaderProtectionKey == "" && existing != nil && r.PostFormValue("obf_hpk_clear") != "1" {
			o.HeaderProtectionKey = existing.HeaderProtectionKey
		}
	}
	for _, field := range []struct {
		key string
		dst *awgparam.U16Range
	}{
		{"obf_padding", &o.ContentPaddingAddition},
		{"obf_rekey_after", &o.RekeyAfterTime},
		{"obf_rekey_timeout", &o.RekeyTimeout},
		{"obf_reject_after", &o.RejectAfterTime},
		{"obf_keepalive", &o.KeepaliveTimeout},
		{"obf_max_handshake", &o.MaxHandshakeAttempts},
	} {
		text := trim(field.key)
		if text == "" {
			continue
		}
		if *field.dst, err = awgparam.ParseU16Range(text); err != nil {
			return iface.Obfuscation{}, domain.E(domain.CodeParamConstraint, "%s must be N or low-high within u16 bounds", field.key)
		}
	}
	o.RandomTrailers = r.PostFormValue("obf_random_trailers") == "1"
	o.DisableCookies = r.PostFormValue("obf_disable_cookies") == "1"
	return o, nil
}

func (s *Server) handleIfaceCreate(w http.ResponseWriter, r *http.Request) {
	port, err := optionalFormInt(r, "listen_port")
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	mtu, err := optionalFormInt(r, "mtu")
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	obfuscation, err := obfuscationFromForm(r, nil)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	policy, generated, err := generatedPolicyFromForm(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	in := iface.CreateInput{
		Name:             strings.TrimSpace(r.PostFormValue("name")),
		ListenPort:       port,
		Subnet:           strings.TrimSpace(r.PostFormValue("subnet")),
		MTU:              mtu,
		Obfuscation:      obfuscation,
		Preset:           policy,
		GeneratedProfile: generated,
		BackendMode:      domain.BackendKernel,
		EndpointOverride: strings.TrimSpace(r.PostFormValue("endpoint_override")),
	}
	i, err := s.Ifaces.Create(r.Context(), in)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "interface.created", i.ID, map[string]any{"name": i.Name})
	s.runReconcile(r)
	s.redirectToast(w, r, "/interfaces", "ifaces.toast.created")
}

func (s *Server) handleIfaceUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prev, err := s.Ifaces.Get(r.Context(), id)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	obfuscation, err := obfuscationFromForm(r, &prev.Obfuscation)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	policy, generated, err := generatedPolicyFromForm(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	in := iface.UpdateInput{
		Obfuscation:      ptrOf(obfuscation),
		GeneratedProfile: generated,
	}
	if generated {
		in.Preset = &policy
	}
	if v := r.PostFormValue("mtu"); v != "" {
		mtu, err := optionalFormInt(r, "mtu")
		if err != nil {
			s.actionFailed(w, r, err)
			return
		}
		in.MTU = &mtu
	}
	if v := r.PostFormValue("endpoint_override"); v != "" {
		in.EndpointOverride = strPtr(strings.TrimSpace(v))
	} else {
		in.EndpointOverride = strPtr("")
	}
	enabled := r.PostFormValue("enabled") != "0"
	in.Enabled = &enabled

	// Rotation awareness: changing the obfuscation profile recreates the
	// tunnel (setconf cannot switch modes in place) and every client config
	// with the old parameters must be re-exported. The toast says so.
	i, err := s.Ifaces.Update(r.Context(), id, in)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "interface.updated", id, map[string]any{"name": i.Name})
	s.runReconcile(r)
	if prev.Obfuscation != i.Obfuscation {
		s.redirectToast(w, r, "/interfaces", "ifaces.toast.rotation")
		return
	}
	s.redirectToast(w, r, "/interfaces", "ifaces.toast.updated")
}

func generatedPolicyFromForm(r *http.Request) (string, bool, error) {
	policy := strings.TrimSpace(r.PostFormValue("profile_policy"))
	switch iface.ProfilePolicy(policy) {
	case iface.ProfileRecommended, iface.ProfileRandomized:
		return policy, true, nil
	default:
		// Empty/plain/custom and legacy labels are inferred from the submitted
		// values. This keeps pre-Phase-8 rows editable while only current
		// server-generated policies receive the trusted classification path.
		return "", false, nil
	}
}

func optionalFormInt(r *http.Request, key string) (int, error) {
	text := strings.TrimSpace(r.PostFormValue(key))
	if text == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, domain.E(domain.CodeInvalidRequest, "%s must be an integer", key)
	}
	return value, nil
}

func (s *Server) handleIfaceEnable(w http.ResponseWriter, r *http.Request) {
	s.ifaceToggle(w, r, true)
}

func (s *Server) handleIfaceDisable(w http.ResponseWriter, r *http.Request) {
	s.ifaceToggle(w, r, false)
}

func (s *Server) ifaceToggle(w http.ResponseWriter, r *http.Request, enable bool) {
	id := r.PathValue("id")
	if _, err := s.Ifaces.Update(r.Context(), id, iface.UpdateInput{Enabled: &enable}); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "interface.updated", id, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/interfaces", "ifaces.toast.toggled")
}

func (s *Server) handleIfaceDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Ifaces.Delete(r.Context(), id); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "interface.deleted", id, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/interfaces", "ifaces.toast.deleted")
}

// ptrOf is a tiny helper for the obfuscation pointer field.
func ptrOf(o iface.Obfuscation) *iface.Obfuscation { return &o }

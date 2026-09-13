package web

import (
	"crypto/subtle"
	"encoding/base64"
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
	I          *iface.Interface
	Devices    int
	CountKnown bool
}

type ifacesData struct {
	Rows    []ifaceRow
	Enabled int
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
	enabled := 0
	rows := make([]ifaceRow, 0, len(ifaces))
	for _, i := range ifaces {
		if i.Enabled {
			enabled++
		}
		rows = append(rows, ifaceRow{I: safeInterfaceView(i), Devices: counts[i.ID], CountKnown: err == nil})
	}
	_ = s.render(w, r, "ifaces", "app", ifacesData{Rows: rows, Enabled: enabled})
}

// ifaceFormData backs the new/edit page. I is nil on create.
type ifaceFormData struct {
	I                      *iface.Interface
	Form                   operationalForm
	HasHeaderProtectionKey bool
}

func (s *Server) handleIfaceNew(w http.ResponseWriter, r *http.Request) {
	data := newIfaceFormData(nil)
	if name, err := s.Ifaces.NextAvailableName(r.Context()); err == nil {
		data.Form.Values["name"] = name
	} else {
		s.logError(r, "suggest interface name", err)
	}
	_ = s.render(w, r, "iface_form", "app", data)
}

func (s *Server) handleIfaceEditPage(w http.ResponseWriter, r *http.Request) {
	i, err := s.Ifaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	_ = s.render(w, r, "iface_form", "app", newIfaceFormData(i))
}

func (s *Server) handleProfilePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	policy := iface.ProfilePolicy(strings.TrimSpace(r.PostFormValue("policy")))
	if !iface.IsGeneratedProfilePolicy(policy) {
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
	token, err := s.sealProfilePreview(r, policy, profile)
	if err != nil {
		s.logError(r, "profile preview seal", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "profile_generation_failed"})
		return
	}
	fields := profileFormFields(profile)
	if policy == iface.ProfileSuggested {
		fields["mtu"] = strconv.Itoa(iface.SuggestedMTU)
	}
	_ = json.NewEncoder(w).Encode(profilePreviewResponse{
		Policy: string(policy),
		Fields: fields,
		Token:  token,
	})
}

type profilePreviewResponse struct {
	Policy string            `json:"policy"`
	Fields map[string]string `json:"fields"`
	Token  string            `json:"token"`
}

type sealedProfilePreview struct {
	Version int                 `json:"v"`
	Policy  iface.ProfilePolicy `json:"policy"`
	Profile iface.Obfuscation   `json:"profile"`
	CSRF    string              `json:"csrf"`
}

func (s *Server) sealProfilePreview(r *http.Request, policy iface.ProfilePolicy, profile iface.Obfuscation) (string, error) {
	if s.Ring == nil {
		return "", domain.E(domain.CodeParamConstraint, "profile preview sealing is unavailable")
	}
	csrf, _ := r.Context().Value(ctxCSRF).(string)
	if csrf == "" {
		return "", domain.E(domain.CodeParamConstraint, "profile preview session binding is unavailable")
	}
	payload, err := json.Marshal(sealedProfilePreview{Version: 1, Policy: policy, Profile: profile, CSRF: csrf})
	if err != nil {
		return "", err
	}
	ciphertext, err := s.Ring.Encrypt(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (s *Server) verifyProfilePreview(r *http.Request, token string, policy iface.ProfilePolicy, profile iface.Obfuscation) error {
	const maxTokenBytes = 16 << 10
	if s.Ring == nil || token == "" || len(token) > maxTokenBytes {
		return domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	}
	payload, err := s.Ring.Decrypt(ciphertext)
	if err != nil {
		return domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	}
	var preview sealedProfilePreview
	if err := json.Unmarshal(payload, &preview); err != nil {
		return domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	}
	csrf, _ := r.Context().Value(ctxCSRF).(string)
	if preview.Version != 1 || preview.Policy != policy || preview.Profile != profile ||
		subtle.ConstantTimeCompare([]byte(preview.CSRF), []byte(csrf)) != 1 {
		return domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	}
	return iface.ValidateGeneratedProfile(policy, profile)
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
	// Native forms post stale fields when a checkbox is unchecked. The toggle
	// is authoritative; ignoring those values matches the enhanced form.
	if !o.Enabled {
		return o, nil
	}
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
		s.ifaceFormError(w, r, nil, err)
		return
	}
	mtu, err := optionalFormInt(r, "mtu")
	if err != nil {
		s.ifaceFormError(w, r, nil, err)
		return
	}
	obfuscation, err := obfuscationFromForm(r, nil)
	if err != nil {
		s.ifaceFormError(w, r, nil, err)
		return
	}
	policy, generated, err := s.generatedPolicyFromForm(r, obfuscation, nil)
	if err != nil {
		s.ifaceFormError(w, r, nil, err)
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
		s.ifaceFormError(w, r, nil, err)
		return
	}
	s.audit(r, "interface.created", i.ID, map[string]any{"name": i.Name})
	s.runReconcile(r)
	s.redirectToast(w, r, operationalReturnPath(r, "/interfaces", "interfaces.read"), "ifaces.toast.created")
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
		s.ifaceFormError(w, r, prev, err)
		return
	}
	policy, generated, err := s.generatedPolicyFromForm(r, obfuscation, prev)
	if err != nil {
		s.ifaceFormError(w, r, prev, err)
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
			s.ifaceFormError(w, r, prev, err)
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
		s.ifaceFormError(w, r, prev, err)
		return
	}
	s.audit(r, "interface.updated", id, map[string]any{"name": i.Name})
	s.runReconcile(r)
	if prev.Obfuscation != i.Obfuscation {
		s.redirectToast(w, r, operationalReturnPath(r, "/interfaces", "interfaces.read"), "ifaces.toast.rotation")
		return
	}
	s.redirectToast(w, r, operationalReturnPath(r, "/interfaces", "interfaces.read"), "ifaces.toast.updated")
}

func (s *Server) generatedPolicyFromForm(r *http.Request, profile iface.Obfuscation, existing *iface.Interface) (string, bool, error) {
	policyText := strings.TrimSpace(r.PostFormValue("profile_policy"))
	policy := iface.ProfilePolicy(policyText)
	switch policy {
	case iface.ProfileRecommended, iface.ProfilePerformance, iface.ProfileBalanced, iface.ProfileResilient, iface.ProfileSuggested, iface.ProfileRandomized:
		if token := strings.TrimSpace(r.PostFormValue("profile_token")); token != "" {
			if err := s.verifyProfilePreview(r, token, policy, profile); err != nil {
				return "", false, err
			}
			return policyText, true, nil
		}
		// Existing generated profiles may be edited for unrelated fields
		// without minting a fresh preview. Preserve the classification only
		// when the submitted AWG values are byte-for-byte unchanged.
		if existing != nil {
			if existing.Preset == policyText && existing.Obfuscation == profile {
				return policyText, true, nil
			}
			// Native forms cannot update the hidden policy when values change.
			// Infer custom/plain through the service; never retain generated
			// provenance for a changed profile without a verified fresh seal.
			return "", false, nil
		}
		return "", false, domain.E(domain.CodeParamConstraint, "generated profile preview is invalid; generate it again")
	default:
		// Empty/plain/custom and legacy labels are inferred from submitted
		// values. Generated classifications require an authenticated sealed
		// preview (or an unchanged already-stored generated profile).
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
	s.redirectToast(w, r, operationalReturnPath(r, "/interfaces", "interfaces.read"), "ifaces.toast.toggled")
}

func (s *Server) handleIfaceDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Ifaces.Delete(r.Context(), id); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "interface.deleted", id, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, operationalReturnPath(r, "/interfaces", "interfaces.read"), "ifaces.toast.deleted")
}

// ptrOf is a tiny helper for the obfuscation pointer field.
func ptrOf(o iface.Obfuscation) *iface.Obfuscation { return &o }

func newIfaceFormData(i *iface.Interface) ifaceFormData {
	values := profileFormFields(iface.Obfuscation{})
	delete(values, "obf_hpk")
	for key, value := range map[string]string{"name": "", "listen_port": "", "subnet": "", "mtu": "", "endpoint_override": "", "enabled": "1", "profile_policy": "plain", "profile_token": "", "obf_hpk_clear": ""} {
		values[key] = value
	}
	if i != nil {
		for key, value := range profileFormFields(i.Obfuscation) {
			if key != "obf_hpk" {
				values[key] = value
			}
		}
		values["name"], values["listen_port"], values["subnet"], values["mtu"] = i.Name, strconv.Itoa(i.ListenPort), i.Subnet, strconv.Itoa(i.MTU)
		values["endpoint_override"], values["profile_policy"] = i.EndpointOverride, i.Preset
		if !i.Enabled {
			values["enabled"] = "0"
		}
	}
	return ifaceFormData{I: safeInterfaceView(i), Form: operationalForm{Values: values}, HasHeaderProtectionKey: i != nil && i.Obfuscation.HeaderProtectionKey != ""}
}

func (s *Server) ifaceFormError(w http.ResponseWriter, r *http.Request, i *iface.Interface, err error) {
	d := newIfaceFormData(i)
	d.Form = submittedOperationalForm(r, d.Form.Values)
	if i != nil {
		d.Form.Values["name"], d.Form.Values["listen_port"], d.Form.Values["subnet"] = i.Name, strconv.Itoa(i.ListenPort), i.Subnet
	}
	if d.Form.V("enabled") != "0" {
		d.Form.Values["enabled"] = "1"
	}
	// A generated secret cannot be redisplayed. Keep its policy but require a
	// fresh preview, so retrying cannot silently downgrade a generated profile.
	d.Form.SecretReset = r.PostFormValue("obf_hpk") != ""
	if d.Form.SecretReset {
		d.Form.Values["profile_token"] = ""
	}
	if key := operationalErrorField(err, false); key != "" {
		d.Form.Fields[key] = "common.error_validation"
	}
	for _, key := range []string{"listen_port", "mtu"} {
		if _, e := optionalFormInt(r, key); e != nil {
			d.Form.Fields[key] = "forms.error.number"
		}
	}
	var prior *iface.Obfuscation
	if i != nil {
		prior = &i.Obfuscation
	}
	if _, e := obfuscationFromForm(r, prior); e != nil {
		if key := operationalErrorField(e, false); key != "" {
			d.Form.Fields[key] = "forms.error.awg"
		}
	}
	s.operationalFormStatus(w, r, &d.Form, err)
	_ = s.render(w, r, "iface_form", "app", d)
}

func (d ifaceFormData) ProfileKey() string {
	switch d.Form.V("profile_policy") {
	case "recommended", "performance", "balanced", "resilient", "suggested", "randomized", "plain", "custom":
		return "ifaces.profile." + d.Form.V("profile_policy")
	}
	if d.Form.V("obf_enabled") == "1" {
		return "ifaces.profile.custom"
	}
	return "ifaces.profile.plain"
}

func (d ifaceFormData) AdvancedOpen() bool {
	if d.Form.Error != "" || d.HasHeaderProtectionKey {
		return true
	}
	for _, key := range []string{"obf_s3", "obf_s4", "obf_i1", "obf_i2", "obf_i3", "obf_i4", "obf_i5", "obf_padding", "obf_rekey_after", "obf_rekey_timeout", "obf_reject_after", "obf_keepalive", "obf_max_handshake", "obf_random_trailers", "obf_disable_cookies"} {
		if v := d.Form.V(key); v != "" && v != "0" {
			return true
		}
	}
	return false
}

// Template contexts carry public configuration and presence flags only, not
// encrypted private-key carriers or plaintext header-protection keys.
func safeInterfaceView(i *iface.Interface) *iface.Interface {
	if i == nil {
		return nil
	}
	view := *i
	view.PrivKeyEnc = nil
	view.Obfuscation.HeaderProtectionKey = ""
	return &view
}

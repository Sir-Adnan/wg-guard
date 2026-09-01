package web

import (
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

// obfuscationFromForm parses the obfuscation section without coercing invalid
// values to zero. Empty fields map to zero so the enabled toggle stays clean;
// relationship validation remains in the interface service.
func obfuscationFromForm(r *http.Request) (iface.Obfuscation, error) {
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
	// Explicit advanced 2.0/3.x parameters (amneziawg.md).
	o.HeaderProtectionKey = trim("obf_hpk")
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
	obfuscation, err := obfuscationFromForm(r)
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
	obfuscation, err := obfuscationFromForm(r)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	in := iface.UpdateInput{
		Obfuscation: ptrOf(obfuscation),
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
	prev, err := s.Ifaces.Get(r.Context(), id)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
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

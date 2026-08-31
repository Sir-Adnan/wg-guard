package web

import (
	"net/http"
	"strconv"
	"strings"

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

// obfuscationFromForm parses the obfuscation section. Empty fields map to
// zero so "enabled" toggles cleanly; validation happens in the service.
func obfuscationFromForm(r *http.Request) iface.Obfuscation {
	o := iface.Obfuscation{Enabled: r.PostFormValue("obf_enabled") == "1"}
	atoi := func(key string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue(key)))
		return n
	}
	trim := func(key string) string { return strings.TrimSpace(r.PostFormValue(key)) }
	o.Jc = atoi("obf_jc")
	o.Jmin = atoi("obf_jmin")
	o.Jmax = atoi("obf_jmax")
	o.S1 = atoi("obf_s1")
	o.S2 = atoi("obf_s2")
	o.H1 = uint32(atoi("obf_h1"))
	o.H2 = uint32(atoi("obf_h2"))
	o.H3 = uint32(atoi("obf_h3"))
	o.H4 = uint32(atoi("obf_h4"))
	// Capability-gated 2.0/3.x parameters (amneziawg.md).
	o.S3 = atoi("obf_s3")
	o.S4 = atoi("obf_s4")
	o.HeaderProtectionKey = trim("obf_hpk")
	o.ContentPaddingAddition = trim("obf_padding")
	o.RekeyAfterTime = trim("obf_rekey_after")
	o.RekeyTimeout = trim("obf_rekey_timeout")
	o.RejectAfterTime = trim("obf_reject_after")
	o.KeepaliveTimeout = trim("obf_keepalive")
	o.MaxHandshakeAttempts = trim("obf_max_handshake")
	o.RandomTrailers = r.PostFormValue("obf_random_trailers") == "1"
	o.DisableCookies = r.PostFormValue("obf_disable_cookies") == "1"
	return o
}

func (s *Server) handleIfaceCreate(w http.ResponseWriter, r *http.Request) {
	port, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("listen_port")))
	mtu, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("mtu")))
	in := iface.CreateInput{
		Name:             strings.TrimSpace(r.PostFormValue("name")),
		ListenPort:       port,
		Subnet:           strings.TrimSpace(r.PostFormValue("subnet")),
		MTU:              mtu,
		Obfuscation:      obfuscationFromForm(r),
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
	in := iface.UpdateInput{
		Obfuscation: ptrOf(obfuscationFromForm(r)),
	}
	if v := r.PostFormValue("mtu"); v != "" {
		if mtu, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			in.MTU = &mtu
		}
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

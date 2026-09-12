package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/clientconf"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/user"
)

// deviceRow is one row of the user-detail device table.
type deviceRow struct {
	D      *deviceView
	Online bool
	LastHS *time.Time
}

// deviceView deliberately excludes encrypted key carriers from templates.
type deviceView struct {
	ID, Name, IPv4   string
	Enabled          bool
	RXBytes, TXBytes uint64
}

func safeDeviceViews(devs []*device.Device) []*deviceView {
	out := make([]*deviceView, 0, len(devs))
	for _, d := range devs {
		out = append(out, &deviceView{ID: d.ID, Name: d.Name, IPv4: d.IPv4, Enabled: d.Enabled, RXBytes: d.RXBytes, TXBytes: d.TXBytes})
	}
	return out
}

// userDetailData feeds the user detail page.
type userDetailData struct {
	Form         operationalForm
	Action       string
	U            *userDetailUser
	Devices      []deviceRow
	PlanName     string
	IfaceName    string
	OnlineWindow int64
	SubExists    bool   // a subscription link row exists
	SubURL       string // public URL ("" when absent or revoked)
	DevicesKnown bool
	PlanKnown    bool
	IfaceKnown   bool
	SubKnown     bool
	SubRevoked   bool
}

// userDetailUser wraps user.User with display helpers that templates cannot
// compute (pointer-free copies of the accumulated counters).
type userDetailUser struct {
	*user.User
	Used     int64
	HasLimit bool
}

// handleUserDetail renders the account page: overview, devices, actions.
func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	data := userDetailData{Form: userActionForm(), OnlineWindow: 180, PlanKnown: u.PlanID == nil, IfaceKnown: u.InterfaceID == nil}
	if v, err := s.Settings.GetInt(ctx, "accounting.online_window_seconds"); err == nil && v > 0 {
		data.OnlineWindow = int64(v)
	}
	cutoff := time.Now().UTC().Add(-time.Duration(data.OnlineWindow) * time.Second)

	if p, err := s.Plans.Get(ctx, deref(u.PlanID)); err == nil && u.PlanID != nil {
		data.PlanName = p.Name
		data.PlanKnown = true
	}
	if f, err := s.Ifaces.Get(ctx, deref(u.InterfaceID)); err == nil && u.InterfaceID != nil {
		data.IfaceName = f.Name
		data.IfaceKnown = true
	}
	if canOperate(r, auth.ScopeDevicesRead) {
		if devs, err := s.Devices.ListForUser(ctx, u.ID); err == nil {
			data.DevicesKnown = true
			data.Devices = make([]deviceRow, 0, len(devs))
			for _, d := range devs {
				row := deviceRow{D: safeDeviceViews([]*device.Device{d})[0]}
				if d.LastHandshake != nil {
					hs := *d.LastHandshake
					row.LastHS = &hs
					row.Online = hs.After(cutoff)
				}
				data.Devices = append(data.Devices, row)
			}
		} else {
			s.logError(r, "device list", err)
		}
	}
	if s.Links != nil && (canOperate(r, auth.ScopeConfigsRead) || canOperate(r, auth.ScopeUsersUpdate)) {
		l, err := s.Links.ForUser(ctx, u.ID)
		data.SubKnown = err == nil
		if err == nil && l != nil {
			data.SubExists = true
			data.SubRevoked = l.Revoked()
			if !l.Revoked() && l.Token != "" && canOperate(r, auth.ScopeConfigsRead) {
				data.SubURL = s.subURLFor(r, l.Token)
			}
		}
	}
	data.U = &userDetailUser{User: u, Used: u.TrafficUsedRX + u.TrafficUsedTX, HasLimit: u.TrafficLimitBytes != nil}
	_ = s.render(w, r, "user_detail", "app", data)
}

// handleDeviceCreate provisions one device with fresh keys.
func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.loadUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.badRequest(w, r, "bad form")
		return
	}
	name := r.PostFormValue("name")
	keys, err := s.generateKeys(r, false)
	if err != nil {
		s.userActionError(w, r, u.ID, "devices", "", err)
		return
	}
	if _, err := s.Devices.Create(r.Context(), u.ID, name, *keys, ""); err != nil {
		field := ""
		if domain.CodeOf(err) == domain.CodeInvalidRequest {
			field = "name"
		}
		s.userActionError(w, r, u.ID, "devices", field, err)
		return
	}
	s.audit(r, "device.created", u.ID, map[string]any{"name": name})
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+u.ID, "devices.toast.created")
}

func (s *Server) handleDeviceEnable(w http.ResponseWriter, r *http.Request) {
	s.deviceToggle(w, r, true)
}

func (s *Server) handleDeviceDisable(w http.ResponseWriter, r *http.Request) {
	s.deviceToggle(w, r, false)
}

func (s *Server) deviceToggle(w http.ResponseWriter, r *http.Request, enable bool) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := s.Devices.SetEnabled(r.Context(), d.ID, enable); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, map[bool]string{true: "device.enabled", false: "device.disabled"}[enable], d.ID, nil)
	s.runReconcile(r)
	if enable {
		s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.enabled")
	} else {
		s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.disabled")
	}
}

func (s *Server) handleDeviceRegenerate(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	keys, err := s.generateKeys(r, false)
	if err != nil {
		s.actionFailed(w, r, err)
		return
	}
	if err := s.Devices.Regenerate(r.Context(), d.ID, *keys); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "device.regenerated", d.ID, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.regenerated")
}

func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDevice(w, r)
	if !ok {
		return
	}
	if err := s.Devices.Delete(r.Context(), d.ID); err != nil {
		s.actionFailed(w, r, err)
		return
	}
	s.audit(r, "device.deleted", d.ID, nil)
	s.runReconcile(r)
	s.redirectToast(w, r, "/users/"+d.UserID, "devices.toast.deleted")
}

// handleDeviceConfig streams the client .conf (no-store — key material).
func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	d, err := s.Devices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeQRError(w, r, err)
		return
	}
	text, err := s.ClientConf.Render(r.Context(), d.ID)
	if err != nil {
		s.writeQRError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+s.configFilename(r, d)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(text))
}

// configFilename builds the download filename for a device config:
// [prefix]username-device[suffix].conf (downloads.filename_* settings).
func (s *Server) configFilename(r *http.Request, d *device.Device) string {
	ctx := r.Context()
	prefix, _ := s.Settings.GetString(ctx, "downloads.filename_prefix")
	suffix, _ := s.Settings.GetString(ctx, "downloads.filename_suffix")
	username := ""
	if u, err := s.Users.Get(ctx, d.UserID); err == nil {
		username = u.Username
	}
	return clientconf.ConfigFilename(prefix, username, d.Name, suffix)
}

// handleDeviceQR streams the client config as a PNG (no-store).
func (s *Server) handleDeviceQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	d, err := s.Devices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, err := s.ClientConf.Render(r.Context(), d.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	png, err := clientconf.QR(text)
	if err != nil {
		s.writeQRError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `inline; filename="`+
		strings.TrimSuffix(s.configFilename(r, d), ".conf")+`.png"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) writeQRError(w http.ResponseWriter, r *http.Request, err error) {
	if domain.CodeOf(err) == domain.CodeInvalidRequest {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	http.NotFound(w, r)
}

// loadDevice fetches the path device or writes the error response.
func (s *Server) loadDevice(w http.ResponseWriter, r *http.Request) (*device.Device, bool) {
	d, err := s.Devices.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if domain.CodeOf(err) == domain.CodeDeviceNotFound {
			s.surfaceError(w, r, http.StatusNotFound, "common.error_not_found", "")
			return nil, false
		}
		s.logError(r, "device load", err)
		s.surfaceError(w, r, http.StatusInternalServerError, "common.error_generic", "")
		return nil, false
	}
	return d, true
}

// generateKeys mints a fresh X25519 keypair with the private half sealed by
// the master key ring (same path as the API; plaintext keys never persist).
func (s *Server) generateKeys(r *http.Request, withPSK bool) (*device.KeyMaterial, error) {
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	privEnc, err := s.Ring.Encrypt([]byte(kp.Private))
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}
	keys := &device.KeyMaterial{PublicKey: kp.Public, PrivateKeyEnc: privEnc}
	if withPSK {
		psk, err := tunnel.GeneratePresharedKey()
		if err != nil {
			return nil, err
		}
		pskEnc, err := s.Ring.Encrypt([]byte(psk))
		if err != nil {
			return nil, fmt.Errorf("encrypt preshared key: %w", err)
		}
		keys.PresharedEnc = pskEnc
	}
	return keys, nil
}

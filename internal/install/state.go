package install

import (
	"strings"
)

// State is privileged deletion and execution input. Only the documented fixed
// layout can be managed; custom/manual layouts require manual migration.
func validateState(st *State) error {
	if st.Schema < 1 || st.Schema > StateSchema || !st.Mode.Valid() {
		return terminalError("install.error.state")
	}
	if st.ConfigPath != ConfigPath || st.DataDir != DataDir || (st.BinPath != "" && st.BinPath != BinPath) || (st.ComposePath != "" && st.ComposePath != ComposePth) || (st.UnitPath != "" && st.UnitPath != UnitPath) {
		return terminalError("install.error.state")
	}
	if st.Mode == ModeDocker && st.ComposePath != ComposePth || st.Mode == ModeNative && (st.BinPath != BinPath || st.UnitPath != UnitPath) {
		return terminalError("install.error.state")
	}
	if st.Schema >= 3 {
		if err := validateExposureState(st.Exposure); err != nil {
			return err
		}
	}
	for _, p := range st.ExtraFiles {
		if p != ModuleAutoLoadPath {
			return terminalError("install.error.state")
		}
	}
	for _, p := range st.PackagesInstalled {
		switch p {
		case "amneziawg-dkms", "amneziawg-tools", "kmod", "dkms", "build-essential", "docker.io", "docker-compose-v2", "iproute2", "nftables", "procps", "ca-certificates", "software-properties-common":
			continue
		}
		if !strings.HasPrefix(p, "linux-headers-") || len(p) > 128 || strings.ContainsAny(p, " /\\\t\r\n=:;") {
			return terminalError("install.error.state")
		}
	}
	for _, a := range []*Artifact{st.Current, st.Previous} {
		if err := validateArtifact(a); err != nil {
			return err
		}
	}
	return nil
}

func validateExposureState(s ExposureState) error {
	if !s.Mode.Valid() || s.BackendPort < 1 || s.BackendPort > 65535 || s.PublicPort < 0 || s.PublicPort > 65535 {
		return terminalError("install.error.state")
	}
	managedPaths := func() bool {
		return s.CertFile == ManagedCertPath && s.KeyFile == ManagedKeyPath && s.DeployHook == CertbotDeployHookPath
	}
	switch s.Mode {
	case ExposurePrivate:
		if s.Certificate != "" || s.PublicURL != "" || s.PublicPort != 0 || s.NginxConfigPath != "" || s.ACMEWebroot != "" || s.CertFile != "" || s.KeyFile != "" || s.DeployHook != "" {
			return terminalError("install.error.state")
		}
	case ExposureDirect:
		if !validHTTPSURL(s.PublicURL) || s.PublicPort < 1 || !s.Certificate.Valid() {
			return terminalError("install.error.state")
		}
		switch s.Certificate {
		case CertificateBuiltin, CertificateManual:
			if s.CertFile != "" || s.KeyFile != "" || s.DeployHook != "" {
				return terminalError("install.error.state")
			}
		case CertificateCloudflareDNS, CertificateIP:
			if !managedPaths() {
				return terminalError("install.error.state")
			}
		default:
			return terminalError("install.error.state")
		}
		if s.NginxConfigPath != "" || s.ACMEWebroot != "" {
			return terminalError("install.error.state")
		}
	case ExposureNginx:
		if !validHTTPSURL(s.PublicURL) || s.PublicPort < 1 || s.NginxConfigPath != NginxConfigPath || s.ACMEWebroot != ACMEWebrootPath {
			return terminalError("install.error.state")
		}
		switch s.Certificate {
		case CertificateWebroot, CertificateCloudflareDNS:
			if !managedPaths() {
				return terminalError("install.error.state")
			}
		case CertificateManual, CertificateCloudflareOrigin:
			if s.CertFile != "" || s.KeyFile != "" || s.DeployHook != "" {
				return terminalError("install.error.state")
			}
		default:
			return terminalError("install.error.state")
		}
	case ExposureExternalProxy:
		if s.Certificate != CertificateExternal || !validHTTPSURL(s.PublicURL) || s.PublicPort < 1 || s.NginxConfigPath != "" || s.ACMEWebroot != "" || s.CertFile != "" || s.KeyFile != "" || s.DeployHook != "" {
			return terminalError("install.error.state")
		}
	}
	return nil
}

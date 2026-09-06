package install

import "strings"

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

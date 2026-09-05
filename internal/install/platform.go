package install

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// PlatformReport is an observation, not a certification of this OS/arch cell.
type PlatformReport struct {
	OS                string `json:"os"`
	Version           string `json:"version"`
	Arch              string `json:"arch"`
	Kernel            string `json:"kernel"`
	Init              string `json:"init"`
	AutomaticPackages bool   `json:"automatic_packages"`
}

// InspectPlatform is read-only and fails closed on unknown OS/architecture/init.
// Only Ubuntu 24.04 has an automatic package adapter; other Linux systems can
// proceed with checked, operator-provisioned prerequisites.
func InspectPlatform(ctx context.Context, h Host) (PlatformReport, error) {
	var r PlatformReport
	osname, err := h.Output(ctx, []string{"uname", "-s"}, 10*time.Second)
	if err != nil || strings.TrimSpace(osname) != "Linux" {
		return r, terminalError("install.error.platform.1")
	}
	data, err := h.ReadFile("/etc/os-release")
	if err != nil {
		return r, terminalError("install.error.platform.2")
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch key {
		case "ID":
			r.OS = value
		case "VERSION_ID":
			r.Version = value
		}
	}
	if r.OS == "" || r.Version == "" {
		return r, terminalError("install.error.platform.3")
	}
	arch, err := h.Output(ctx, []string{"uname", "-m"}, 10*time.Second)
	if err != nil {
		return r, terminalError("install.error.platform.4")
	}
	switch strings.TrimSpace(arch) {
	case "x86_64":
		r.Arch = "amd64"
	case "aarch64", "arm64":
		r.Arch = "arm64"
	default:
		return r, terminalError("install.error.platform.5")
	}
	kernel, err := h.Output(ctx, []string{"uname", "-r"}, 10*time.Second)
	if err != nil || !safeKernel(strings.TrimSpace(kernel)) {
		return r, terminalError("install.error.platform.6")
	}
	r.Kernel = strings.TrimSpace(kernel)
	init, err := h.ReadFile("/proc/1/comm")
	if err == nil {
		r.Init = strings.TrimSpace(string(init))
	}
	r.AutomaticPackages = r.OS == "ubuntu" && r.Version == "24.04" && r.Init == "systemd"
	return r, nil
}

func safeKernel(s string) bool {
	if s == "" || len(s) > 128 || s[0] == '-' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".+_-", c)) {
			return false
		}
	}
	return true
}

// resolveEndpoint uses only global interface addresses. Hosts behind NAT must
// supply --public-ip; we do not trust a third-party IP echo service implicitly.
func resolveEndpoint(ctx context.Context, h Host, p *Plan) error {
	if p.VPNEndpoint() != "" {
		return nil
	}
	raw, err := h.Output(ctx, []string{"ip", "-j", "address", "show", "scope", "global"}, 10*time.Second)
	if err == nil {
		var links []struct {
			Addresses []struct {
				Local string `json:"local"`
			} `json:"addr_info"`
		}
		if json.Unmarshal([]byte(raw), &links) == nil {
			for _, link := range links {
				for _, addr := range link.Addresses {
					if validPublicIP(addr.Local) {
						p.PublicIP = addr.Local
						return nil
					}
				}
			}
		}
	}
	return terminalError("install.error.platform.7")
}

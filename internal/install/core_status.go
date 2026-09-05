package install

import (
	"context"
	"strings"
	"time"
)

// InspectInstalledCore reads tools from the deployment's actual runtime while
// keeping kernel observations on the host. It never certifies a Git revision
// from module version strings and never starts a container for inspection.
func InspectInstalledCore(ctx context.Context, h Host) (CoreReport, error) {
	st, err := LoadState(h)
	if err != nil {
		return CoreReport{}, err
	}
	b, _ := SelectCore("recommended")
	if st != nil && st.Core.Requested.ID != "" {
		b = st.Core.Requested
	}
	r := InspectCore(ctx, h, b)
	if st == nil {
		return r, nil
	}
	r.ExternalModule = st.Core.ExternalModule
	if st.Mode == ModeDocker {
		r.ToolsLocation = "container"
		r.ToolsVersion = ""
		r.ToolsPackage = ""
		if raw, err := h.Output(ctx, []string{"docker", "exec", Container, "awg", "--version"}, 15*time.Second); err == nil {
			r.ToolsVersion = strings.TrimSpace(raw)
		}
		if raw, err := h.Output(ctx, []string{"docker", "exec", Container, "dpkg-query", "-W", "-f=${db:Status-Status}\t${Version}", "amneziawg-tools"}, 15*time.Second); err == nil {
			status, version, ok := strings.Cut(strings.TrimSpace(raw), "\t")
			if ok && status == "installed" {
				r.ToolsPackage = version
			}
		}
	}
	return r, nil
}

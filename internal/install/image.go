package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

// BuildRuntimeImage consumes an acquired artifact without executing it. The
// caller owns stagingParent; only our random private child is ever removed.
// A successful return is an immutable local Docker image ID, never a tag.
// M3 owns selecting this image for install/update and recording it in State.
func BuildRuntimeImage(ctx context.Context, h Host, build distribution.Build, b CoreBundle, stagingParent string) (string, error) {
	selected, err := SelectCore(b.ID)
	if err != nil || selected != b {
		return "", terminalError("install.error.image.1")
	}
	if !filepath.IsAbs(stagingParent) || !filepath.IsAbs(build.BinaryPath) || !hexLength(build.SHA256, 64) || !hexLength(build.Commit, 40) {
		return "", terminalError("install.error.image.2")
	}
	info, err := os.Stat(stagingParent)
	if err != nil || !info.IsDir() {
		return "", terminalError("install.error.image.3")
	}
	dir, err := os.MkdirTemp(stagingParent, "wg-guard-runtime-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	source, err := os.Open(build.BinaryPath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", terminalError("install.error.image.4")
	}
	target, err := os.OpenFile(filepath.Join(dir, "wg-guard"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(target, digest), io.LimitReader(source, 256<<20+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || n > 256<<20 || hex.EncodeToString(digest.Sum(nil)) != build.SHA256 {
		return "", terminalError("install.error.image.5")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(runtimeDockerfile(b)), 0o600); err != nil {
		return "", err
	}
	iid := filepath.Join(dir, "image-id")
	args := []string{"docker", "build", "--iidfile", iid, "--label", "org.opencontainers.image.revision=" + build.Commit, "--label", "io.wg-guard.binary.sha256=" + build.SHA256, "--label", "io.wg-guard.core.bundle=" + b.ID, dir}
	if err := h.Run(ctx, args, longTimeout); err != nil {
		return "", terminalError("install.error.image.6", err)
	}
	file, err := os.Open(iid)
	if err != nil {
		return "", terminalError("install.error.image.7")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !strings.HasPrefix(id, "sha256:") || !hexLength(strings.TrimPrefix(id, "sha256:"), 64) {
		return "", terminalError("install.error.image.8")
	}
	return id, nil
}

func hexLength(s string, n int) bool {
	if len(s) != n || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func runtimeDockerfile(b CoreBundle) string {
	return `FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates nftables iproute2 procps curl gnupg software-properties-common \
 && add-apt-repository -y ppa:amnezia/ppa \
 && apt-get update \
 && apt-get install -y --no-install-recommends amneziawg-tools=` + b.ToolsPackage + ` \
 && apt-get purge -y --auto-remove gnupg software-properties-common \
 && rm -rf /var/lib/apt/lists/*
COPY wg-guard /usr/local/bin/wg-guard
ENV WGG_IN_CONTAINER=1
ENTRYPOINT ["/usr/local/bin/wg-guard"]
CMD ["serve", "-config", "/etc/wg-guard/wg-guard.toml"]
`
}

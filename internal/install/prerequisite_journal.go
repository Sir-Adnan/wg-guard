package install

import (
	"context"
	"strings"
	"time"
)

// Intent is durable before apt can partially install a package or shared source.
type journalHost struct {
	Host
	j *Journal
}

func (h journalHost) Run(ctx context.Context, args []string, timeout time.Duration) error {
	if len(args) > 1 && args[0] == "apt-get" && args[1] == "install" {
		for _, arg := range args[2:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			name, _, _ := strings.Cut(arg, "=")
			h.j.PackageIntents = addUnique(h.j.PackageIntents, name)
		}
	}
	if len(args) > 0 && args[0] == "add-apt-repository" {
		h.j.RepositoryIntent = true
	}
	if err := h.j.save(h.Host, "prerequisites"); err != nil {
		return err
	}
	err := h.Host.Run(ctx, args, timeout)
	if saveErr := h.j.save(h.Host, "prerequisites"); saveErr != nil {
		return saveErr
	}
	return err
}

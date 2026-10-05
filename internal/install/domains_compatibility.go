package install

import (
	"errors"
	"fmt"
	"io/fs"
)

func checkDomainRuntimeCompatibility(h Host, c Contract) error {
	cfg, err := ReadBootConfig(h, ConfigPath)
	if err != nil {
		return err
	}
	_, policyErr := readDomainPolicy(h)
	if policyErr != nil && !errors.Is(policyErr, fs.ErrNotExist) {
		return policyErr
	}
	if (cfg.TLS.PolicyFile != "" || policyErr == nil) && c.DomainProtocol != 1 {
		return fmt.Errorf("runtime does not support active domain isolation; remove/review managed domains before selecting this artifact")
	}
	return nil
}

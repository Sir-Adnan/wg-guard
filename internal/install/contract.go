package install

import (
	"context"
	"encoding/json"
	"time"
)

// DataContract changes whenever old code would interpret current data differently,
// even if SQLite accepts its queries (for example pre-0007 H-range truncation).
type Contract struct {
	Revision           int    `json:"revision"`
	DataContract       string `json:"data_contract"`
	Prerequisites      bool   `json:"prerequisites"`
	Recovery           bool   `json:"recovery"`
	LocalOwner         bool   `json:"local_owner"`
	CoordinatedRestore bool   `json:"coordinated_restore"`
	DataLease          bool   `json:"data_lease"`
}

func CurrentContract() Contract {
	return Contract{Revision: 1, DataContract: "schema7-h-ranges-v1", Prerequisites: true, Recovery: true, LocalOwner: true, CoordinatedRestore: true, DataLease: true}
}
func CheckContract(c Contract) error {
	if !knownDataContract(c) || !c.Prerequisites || !c.Recovery || !c.LocalOwner || !c.CoordinatedRestore || !c.DataLease {
		return terminalError("install.error.contract")
	}
	return nil
}
func inspectContract(ctx context.Context, h Host, args []string) (Contract, error) {
	c, err := readContract(ctx, h, args)
	if err != nil {
		return c, err
	}
	return c, CheckContract(c)
}

// Data compatibility and candidate admission are distinct: old artifacts can
// understand the same schema while lacking today's fresh-install capabilities.
func knownDataContract(c Contract) bool { return c.Revision == 1 && c.DataContract != "" }
func readContract(ctx context.Context, h Host, args []string) (Contract, error) {
	raw, err := h.Output(ctx, append(args, "installer-contract"), 15*time.Second)
	if err != nil {
		return Contract{}, terminalError("install.error.contract")
	}
	if len(raw) > 4096 {
		return Contract{}, terminalError("install.error.contract")
	}
	var c Contract
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Contract{}, terminalError("install.error.contract")
	}
	if !knownDataContract(c) {
		return Contract{}, terminalError("install.error.contract")
	}
	return c, nil
}

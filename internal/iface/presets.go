package iface

// Presets are named obfuscation parameter sets applied at profile creation.
// Every value is a *recommended default* (configurable per profile after
// creation); they are starting points chosen from the AmneziaWG community's
// commonly used ranges, not verified optima — final guidance follows the
// Phase 8 VPS matrix (docs/product/requirements.md).

// Preset is a named starting point for a profile's obfuscation parameters.
type Preset struct {
	Name        string
	Obfuscation Obfuscation
	Description string
}

// Presets available in installer/panel pickers. "plain" produces a stock
// WireGuard-compatible configuration (all params omitted upstream).
func Presets() []Preset {
	return []Preset{
		{
			Name:        "plain",
			Description: "No obfuscation — stock WireGuard clients connect",
			Obfuscation: Obfuscation{Enabled: false},
		},
		{
			Name:        "balanced",
			Description: "Recommended default obfuscation for most ISPs",
			Obfuscation: Obfuscation{
				Enabled: true,
				Jc:      4, Jmin: 40, Jmax: 70,
				S1: 15, S2: 64,
				H1: 1, H2: 2, H3: 3, H4: 4,
			},
		},
		{
			Name:        "strong",
			Description: "Heavier junk traffic and distinct client ports",
			Obfuscation: Obfuscation{
				Enabled: true,
				Jc:      12, Jmin: 50, Jmax: 1000,
				S1: 100, S2: 200,
				H1: 1234567, H2: 7654321, H3: 11223344, H4: 55667788,
			},
		},
	}
}

// PresetByName returns the named preset (ok=false when unknown).
func PresetByName(name string) (Preset, bool) {
	for _, p := range Presets() {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

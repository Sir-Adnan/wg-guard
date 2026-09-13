package iface

// Presets are the product-visible generation policies. Values are generated
// by ProfileGenerator; this metadata intentionally contains no reusable magic
// headers or browser-owned randomness.

// Preset is a named starting point for a profile's obfuscation parameters.
type Preset struct {
	Name        ProfilePolicy
	Description string
}

// Presets returns the policies available in API/panel pickers.
func Presets() []Preset {
	return []Preset{
		{
			Name:        ProfilePlain,
			Description: "No obfuscation — stock WireGuard clients connect",
		},
		{
			Name:        ProfileRecommended,
			Description: "Legacy safe defaults with unique per-profile headers",
		},
		{
			Name:        ProfilePerformance,
			Description: "Low-overhead advanced profile for ordinary filtering",
		},
		{
			Name:        ProfileBalanced,
			Description: "Moderate packet variation and advanced transport protection",
		},
		{
			Name:        ProfileResilient,
			Description: "Stronger packet variation for restrictive networks",
		},
		{
			Name:        ProfileSuggested,
			Description: "Expert starting values with a fresh header-protection key",
		},
		{
			Name:        ProfileRandomized,
			Description: "Relationship-aware generated ranges and header protection",
		},
	}
}

// PresetByName returns the named preset (ok=false when unknown).
func PresetByName(name ProfilePolicy) (Preset, bool) {
	for _, p := range Presets() {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

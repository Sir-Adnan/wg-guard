package routing

import "strings"

// Region lists the site test judges an exit against. Services publish where
// they work; these are snapshots of those pages, so a changed list shows up
// as a wrong verdict for one country, never as a crash. Review them with the
// service markers (docs/architecture/egress-routing.md, Site test).

// geminiUnsupported are the countries (ISO 3166 alpha-3, as the app page
// reports them) missing from Google's "Where you can use the Gemini web app"
// list, reviewed 2026-10-19. Mainland China is listed for Workspace only.
var geminiUnsupported = map[string]bool{
	"AFG": true, "BLR": true, "CHN": true, "CUB": true, "IRN": true, "PRK": true, "RUS": true, "SYR": true,
}

// anthropicSupported are the countries (ISO 3166 alpha-2, as Cloudflare
// reports them) on Anthropic's Supported Regions Policy, reviewed 2026-10-19
// (the version effective 2026-11-12), plus the territories of listed
// countries, which the policy also covers.
var anthropicSupported = codeSet(
	// Listed countries.
	"AD AE AG AL AM AO AR AT AU AZ BA BB BD BE BF BG BH BI BJ BN BO BR BS BT BW BZ CA CD CF CG CH CI CL CM CO CR CV CY CZ " +
		"DE DJ DK DM DO DZ EC EE EG ER ES ET FI FJ FM FR GA GB GD GE GH GM GN GQ GR GT GW GY HN HR HT HU ID IE IL IN IQ IS IT " +
		"JM JO JP KE KG KH KI KM KN KR KW KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MG MH MK ML MN MR MT MU MV MW MX MY MZ " +
		"NA NE NG NI NL NO NP NR NZ OM PA PE PG PH PK PL PS PT PW PY QA RO RS RW SA SB SC SD SE SG SI SK SL SM SN SO SR SS ST SV SZ " +
		"TD TG TH TJ TL TM TN TO TR TT TV TW TZ UA UG US UY UZ VA VC VN VU WS ZA ZM ZW " +
		// Territories: United States, France, United Kingdom, Netherlands,
		// Denmark, Finland, Australia, New Zealand, Norway.
		"AS GU MP PR UM VI BL GF GP MF MQ NC PF PM RE TF WF YT AI BM FK GG GI GS IM IO JE KY MS PN SH TC VG " +
		"AW BQ CW SX FO GL AX CC CX HM NF CK NU TK BV SJ",
)

func codeSet(codes string) map[string]bool {
	out := map[string]bool{}
	for _, c := range strings.Fields(codes) {
		out[c] = true
	}
	return out
}

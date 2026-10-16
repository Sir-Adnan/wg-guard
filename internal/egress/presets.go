package egress

import "sort"

// Preset is a curated, built-in service list. Presets are deliberately narrow
// (one service each) so an operator can route only Gemini, or Gemini plus an
// IP-lookup service, without sending all of Google through an outbound.
// Broader coverage comes from geosite categories or custom lines.
type Preset struct {
	ID       string
	Name     string // brand name; not translated
	Category PresetCategory
	Lines    []string // ParseMatchers syntax
}

// PresetCategory groups presets in the rule editor.
type PresetCategory string

const (
	CategoryAI      PresetCategory = "ai"
	CategoryGoogle  PresetCategory = "google"
	CategoryMedia   PresetCategory = "media"
	CategoryDev     PresetCategory = "dev"
	CategoryIPCheck PresetCategory = "ipcheck"
)

// PresetCategories is the display order.
var PresetCategories = []PresetCategory{CategoryAI, CategoryGoogle, CategoryMedia, CategoryDev, CategoryIPCheck}

// Domain lists were reviewed 2026-10 against the services' public endpoints
// and the v2fly domain-list-community categories of the same names. They are
// starting points: operators can add lines to any rule.
var presets = []Preset{
	// AI services restrict by exit IP reputation and country first.
	{ID: "gemini", Name: "Google Gemini", Category: CategoryAI, Lines: []string{
		"gemini.google.com", "gemini.google", "bard.google.com", "aistudio.google.com", "ai.google.dev",
		"makersuite.google.com", "generativelanguage.googleapis.com", "alkalimakersuite-pa.clients6.google.com",
		"alkalicore-pa.clients6.google.com", "aisandbox-pa.googleapis.com", "robinfrontend-pa.googleapis.com",
		"proactivebackend-pa.googleapis.com", "geller-pa.googleapis.com", "assistant-s3-pa.googleapis.com",
		"notebooklm.google", "notebooklm.google.com", "labs.google", "deepmind.com", "deepmind.google",
	}},
	{ID: "openai", Name: "ChatGPT / OpenAI", Category: CategoryAI, Lines: []string{
		"openai.com", "chatgpt.com", "chat.com", "oaistatic.com", "oaiusercontent.com", "sora.com",
		"full:chatgpt.livekit.cloud", "full:openaiapi-site.azureedge.net", "full:production-openaicom-storage.azureedge.net",
	}},
	{ID: "claude", Name: "Claude", Category: CategoryAI, Lines: []string{
		"anthropic.com", "claude.ai", "claude.com", "claudeusercontent.com", "clau.de",
	}},
	{ID: "copilot", Name: "Microsoft Copilot", Category: CategoryAI, Lines: []string{
		"copilot.microsoft.com", "copilot.cloud.microsoft", "full:sydney.bing.com", "full:edgeservices.bing.com",
	}},
	{ID: "github-copilot", Name: "GitHub Copilot", Category: CategoryAI, Lines: []string{
		"githubcopilot.com", "full:copilot-proxy.githubusercontent.com", "full:origin-tracker.githubusercontent.com",
	}},
	{ID: "perplexity", Name: "Perplexity", Category: CategoryAI, Lines: []string{
		"perplexity.ai", "perplexity.com", "pplx.ai",
	}},
	{ID: "grok", Name: "Grok", Category: CategoryAI, Lines: []string{
		"x.ai", "grok.com", "full:grok.x.com",
	}},
	{ID: "meta-ai", Name: "Meta AI", Category: CategoryAI, Lines: []string{"meta.ai"}},
	{ID: "mistral", Name: "Mistral", Category: CategoryAI, Lines: []string{"mistral.ai"}},
	{ID: "poe", Name: "Poe", Category: CategoryAI, Lines: []string{"poe.com", "poecdn.net"}},
	{ID: "character-ai", Name: "Character.AI", Category: CategoryAI, Lines: []string{"character.ai"}},
	{ID: "cursor", Name: "Cursor", Category: CategoryAI, Lines: []string{"cursor.com", "cursor.sh", "cursor-cdn.com"}},

	{ID: "google-play", Name: "Google Play", Category: CategoryGoogle, Lines: []string{
		"play.google.com", "play.googleapis.com", "play-fe.googleapis.com", "full:android.clients.google.com",
		"full:play-lh.googleusercontent.com",
	}},
	{ID: "android-dev", Name: "Android & Firebase", Category: CategoryGoogle, Lines: []string{
		"developer.android.com", "dl.google.com", "dl-ssl.google.com", "maven.google.com",
		"android.googlesource.com", "firebase.google.com", "console.firebase.google.com",
	}},
	{ID: "google-cloud", Name: "Google Cloud", Category: CategoryGoogle, Lines: []string{
		"cloud.google.com", "console.cloud.google.com",
	}},

	{ID: "spotify", Name: "Spotify", Category: CategoryMedia, Lines: []string{
		"spotify.com", "scdn.co", "spotifycdn.com", "spotifycdn.net", "spotify.design", "spotilocal.com",
		"spoti.fi", "spotify.link", "byspotify.com", "tospotify.com", "pscdn.co", "spotifycharts.com",
	}},
	{ID: "netflix", Name: "Netflix", Category: CategoryMedia, Lines: []string{
		"netflix.com", "netflix.net", "nflxext.com", "nflximg.com", "nflximg.net", "nflxso.net",
		"nflxvideo.net", "nflxsearch.net", "fast.com",
	}},
	{ID: "youtube", Name: "YouTube", Category: CategoryMedia, Lines: []string{
		"youtube.com", "youtu.be", "yt.be", "ytimg.com", "googlevideo.com", "youtube-nocookie.com",
		"ggpht.com", "full:youtubei.googleapis.com",
	}},
	{ID: "disney", Name: "Disney+", Category: CategoryMedia, Lines: []string{
		"disneyplus.com", "disney-plus.net", "bamgrid.com", "dssott.com",
	}},

	{ID: "docker", Name: "Docker Hub", Category: CategoryDev, Lines: []string{"docker.io", "docker.com"}},
	{ID: "huggingface", Name: "Hugging Face", Category: CategoryDev, Lines: []string{"huggingface.co", "hf.co"}},
	{ID: "jetbrains", Name: "JetBrains", Category: CategoryDev, Lines: []string{"jetbrains.com", "jb.gg"}},
	{ID: "gradle", Name: "Gradle", Category: CategoryDev, Lines: []string{"gradle.org", "gradle.com"}},

	// Sites compare the exit IP of their own requests with what these lookup
	// services report; routing both through the same exit keeps them consistent.
	{ID: "ip-lookup", Name: "IP lookup services", Category: CategoryIPCheck, Lines: []string{
		"ipdata.co", "ipinfo.io", "ip-api.com", "ipapi.co", "ipify.org", "ifconfig.me", "ifconfig.co",
		"icanhazip.com", "ipregistry.co", "ipgeolocation.io", "ip.sb", "ipwho.is", "ipwhois.app",
		"iplocation.net", "whatismyipaddress.com", "myip.com", "db-ip.com", "maxmind.com", "geojs.io",
		"ip2location.com", "ipquery.io", "iphub.info", "ipqualityscore.com", "scamalytics.com", "browserleaks.com",
	}},
}

var presetByID = func() map[string]*Preset {
	m := make(map[string]*Preset, len(presets))
	for i := range presets {
		m[presets[i].ID] = &presets[i]
	}
	return m
}()

// Presets returns the catalog in display order (category order, then the
// curated order inside each category).
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	rank := map[PresetCategory]int{}
	for i, c := range PresetCategories {
		rank[c] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Category] < rank[out[j].Category] })
	return out
}

// PresetByID returns one preset.
func PresetByID(id string) (Preset, bool) {
	p, ok := presetByID[id]
	if !ok {
		return Preset{}, false
	}
	return *p, true
}

// PresetMatchers expands preset IDs into matchers. Unknown IDs are skipped
// (a preset retired by an upgrade must not break routing).
func PresetMatchers(ids []string) []Matcher {
	var out []Matcher
	seen := map[Matcher]bool{}
	for _, id := range ids {
		p, ok := presetByID[id]
		if !ok {
			continue
		}
		for _, line := range p.Lines {
			m, err := parseMatcher(line)
			if err != nil || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

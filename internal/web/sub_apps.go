package web

// The public subscription page recommends client apps per platform. Links
// point at each project's own store listing, release page or download page
// (reviewed 2026-10-16 from the projects' READMEs: amneziawg-android ff15093,
// wgtunnel 4d0ca4f, amnezia-client 30ff345). No third-party mirrors.

// subAppLink is one download destination.
type subAppLink struct {
	LabelKey string
	URL      string
}

// subApp is one recommended client.
type subApp struct {
	ID       string // amneziawg | wgtunnel | amneziavpn | awg-tools
	Name     string
	BadgeKey string // "" | publicsub.app.recommended | publicsub.app.simple | publicsub.app.advanced
	DescKey  string
	Import   string // conf (file/QR) | key (vpn://) | cli
	Links    []subAppLink
}

// subPlatform is one tab of the setup guide.
type subPlatform struct {
	ID       string // android | ios | windows | macos | linux
	LabelKey string
	Icon     string
	Apps     []subApp
}

const (
	urlAmneziaWGPlay      = "https://play.google.com/store/apps/details?id=org.amnezia.awg"
	urlAmneziaWGAndroid   = "https://github.com/amnezia-vpn/amneziawg-android/releases/latest"
	urlAmneziaWGWindows   = "https://github.com/amnezia-vpn/amneziawg-windows-client/releases/latest"
	urlAmneziaWGApple     = "https://docs.amnezia.org/documentation/amnezia-wg/"
	urlAmneziaWGTools     = "https://github.com/amnezia-vpn/amneziawg-tools"
	urlWGTunnelPlay       = "https://play.google.com/store/apps/details?id=com.zaneschepke.wireguardautotunnel"
	urlWGTunnelSite       = "https://wgtunnel.com"
	urlAmneziaVPNDownload = "https://amnezia.org/en/downloads"
)

var (
	appAmneziaWGAndroid = subApp{ID: "amneziawg", Name: "AmneziaWG", BadgeKey: "publicsub.app.recommended", DescKey: "publicsub.app.amneziawg_desc", Import: "conf",
		Links: []subAppLink{{"publicsub.store.play", urlAmneziaWGPlay}, {"publicsub.store.apk", urlAmneziaWGAndroid}}}
	appAmneziaWGApple = subApp{ID: "amneziawg", Name: "AmneziaWG", BadgeKey: "publicsub.app.recommended", DescKey: "publicsub.app.amneziawg_desc", Import: "conf",
		Links: []subAppLink{{"publicsub.store.website", urlAmneziaWGApple}}}
	appAmneziaWGWindows = subApp{ID: "amneziawg", Name: "AmneziaWG", BadgeKey: "publicsub.app.recommended", DescKey: "publicsub.app.amneziawg_desc", Import: "conf",
		Links: []subAppLink{{"publicsub.store.download", urlAmneziaWGWindows}}}
	appWGTunnelAndroid = subApp{ID: "wgtunnel", Name: "WG Tunnel", DescKey: "publicsub.app.wgtunnel_desc", Import: "conf",
		Links: []subAppLink{{"publicsub.store.play", urlWGTunnelPlay}, {"publicsub.store.website", urlWGTunnelSite}}}
	appWGTunnelDesktop = subApp{ID: "wgtunnel", Name: "WG Tunnel", DescKey: "publicsub.app.wgtunnel_desc", Import: "conf",
		Links: []subAppLink{{"publicsub.store.website", urlWGTunnelSite}}}
	appAmneziaVPN = subApp{ID: "amneziavpn", Name: "AmneziaVPN", BadgeKey: "publicsub.app.simple", DescKey: "publicsub.app.amneziavpn_desc", Import: "key",
		Links: []subAppLink{{"publicsub.store.website", urlAmneziaVPNDownload}}}
	appAWGTools = subApp{ID: "awg-tools", Name: "amneziawg-tools", BadgeKey: "publicsub.app.advanced", DescKey: "publicsub.app.awgtools_desc", Import: "cli",
		Links: []subAppLink{{"publicsub.store.source", urlAmneziaWGTools}}}
)

// subPlatforms is the guide in display order.
var subPlatforms = []subPlatform{
	{ID: "android", LabelKey: "publicsub.os.android", Icon: "smartphone", Apps: []subApp{appAmneziaWGAndroid, appWGTunnelAndroid, appAmneziaVPN}},
	{ID: "ios", LabelKey: "publicsub.os.ios", Icon: "apple", Apps: []subApp{appAmneziaWGApple, appAmneziaVPN}},
	{ID: "windows", LabelKey: "publicsub.os.windows", Icon: "monitor", Apps: []subApp{appAmneziaWGWindows, appWGTunnelDesktop, appAmneziaVPN}},
	{ID: "macos", LabelKey: "publicsub.os.macos", Icon: "laptop", Apps: []subApp{appAmneziaWGApple, appWGTunnelDesktop, appAmneziaVPN}},
	{ID: "linux", LabelKey: "publicsub.os.linux", Icon: "server", Apps: []subApp{appWGTunnelDesktop, appAmneziaVPN, appAWGTools}},
}

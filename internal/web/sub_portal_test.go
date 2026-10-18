package web

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSubscriptionMachineEndpointsAndGuide(t *testing.T) {
	e := newEnv(t)
	userID, deviceID, _, _ := e.seedUserWithDevice()
	link, err := e.srv.Links.ForUser(context.Background(), userID)
	if err != nil || link == nil {
		t.Fatal(err)
	}
	sub := e.subBase(link.Token)

	page := e.get(sub+"?lang=en", nil)
	body := page.Body.String()
	for _, want := range []string{"Install and connect", "AmneziaWG", "WG Tunnel", "AmneziaVPN", `id="os-android"`, "Open in AmneziaVPN", "Time left",
		"https://play.google.com/store/apps/details?id=org.amnezia.awg", `rel="noopener noreferrer"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("sub page missing %q", want)
		}
	}
	if !strings.HasPrefix(page.Header().Get("Subscription-Userinfo"), "upload=") {
		t.Fatalf("userinfo header: %q", page.Header().Get("Subscription-Userinfo"))
	}
	if strings.Contains(body, "vpn://") {
		t.Fatal("the page must not embed key material")
	}

	rec := e.get(sub+"/info", nil)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("info: %d %v", rec.Code, rec.Header())
	}
	var info subInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Username != "alice" || len(info.Devices) != 1 || info.Devices[0].ID != deviceID || !strings.HasSuffix(info.Devices[0].ConfigURL, "/devices/"+deviceID+"/config") {
		t.Fatalf("info body: %+v", info)
	}
	if strings.Contains(rec.Body.String(), "PrivateKey") {
		t.Fatal("info must not carry key material")
	}

	redirect := e.get(sub+"/devices/"+deviceID+"/amneziavpn", nil)
	if redirect.Code != http.StatusSeeOther || !strings.HasPrefix(redirect.Header().Get("Location"), "vpn://") {
		t.Fatalf("amneziavpn redirect: %d %q", redirect.Code, redirect.Header().Get("Location"))
	}
	text := e.get(sub+"/devices/"+deviceID+"/amneziavpn?format=text", nil)
	key := text.Body.String()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, "vpn://"))
	if err != nil || len(raw) < 5 {
		t.Fatalf("key encoding: %v", err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		t.Fatal(err)
	}
	conf, _ := io.ReadAll(zr)
	cfg := e.get(sub+"/devices/"+deviceID+"/config", nil).Body.String()
	if string(conf) != cfg {
		t.Fatal("vpn:// key must carry exactly the downloadable config")
	}
	if rec := e.get("/sub/unknown-token/info", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token info: %d", rec.Code)
	}
	if rec := e.get(sub+"/devices/not-mine/amneziavpn", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign device: %d", rec.Code)
	}

	if err := e.reg.Set(context.Background(), "subscription.layout", "portal"); err != nil {
		t.Fatal(err)
	}
	if body := e.get(sub, nil).Body.String(); !strings.Contains(body, `data-sub-layout="portal"`) || !strings.Contains(body, "/css/app.css") {
		t.Fatal("portal layout not applied")
	}
}

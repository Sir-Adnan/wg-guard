package warp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegisterAndLicenseFlow(t *testing.T) {
	var gotRegister map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("CF-Client-Version") != clientVersion || r.Header.Get("User-Agent") != userAgent {
			http.Error(w, "client identity", http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/"+APIVersion+"/reg":
			_ = json.NewDecoder(r.Body).Decode(&gotRegister)
			_, _ = w.Write([]byte(`{"id":"dev-1","token":"tok-1","account":{"account_type":"free","warp_plus":false},
				"config":{"peers":[{"public_key":"bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=",
				"endpoint":{"host":"engage.cloudflareclient.com:2408","v4":"162.159.192.1:0"}}],
				"interface":{"addresses":{"v4":"172.16.0.2","v6":"2606:4700::1"}}}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/"+APIVersion+"/reg/dev-1/account":
			if r.Header.Get("Authorization") != "Bearer tok-1" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/"+APIVersion+"/reg/dev-1/account":
			_, _ = w.Write([]byte(`{"account_type":"unlimited","warp_plus":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New()
	c.Base, c.HTTP = srv.URL, srv.Client()
	c.Now = func() time.Time { return time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC) }
	reg, err := c.Register(context.Background(), "pub")
	if err != nil {
		t.Fatal(err)
	}
	if reg.DeviceID != "dev-1" || reg.Endpoint != "engage.cloudflareclient.com:2408" || reg.AddressV4 != "172.16.0.2" || reg.Plus {
		t.Fatalf("registration: %+v", reg)
	}
	if gotRegister["key"] != "pub" || gotRegister["tos"] != "2026-10-16T01:02:03.000Z" || gotRegister["tunnel_type"] != "wireguard" {
		t.Fatalf("request body: %v", gotRegister)
	}
	kind, plus, err := c.ApplyLicense(context.Background(), reg.DeviceID, reg.Token, "a1b2c3d4-e5f6a7b8-c9d0e1f2")
	if err != nil || kind != "unlimited" || !plus {
		t.Fatalf("license: %q %v %v", kind, plus, err)
	}
	if _, _, err := c.ApplyLicense(context.Background(), reg.DeviceID, "wrong", "x"); err == nil {
		t.Fatal("rejected license call must fail")
	}
}

func TestEndpointSelection(t *testing.T) {
	for _, tc := range []struct {
		in   endpoint
		want string
	}{
		{endpoint{Host: "engage.cloudflareclient.com:2408"}, "engage.cloudflareclient.com:2408"},
		{endpoint{V4: "162.159.192.1:0"}, "162.159.192.1:2408"},
		{endpoint{V4: "162.159.192.1:0", Ports: []int32{500}}, "162.159.192.1:500"},
	} {
		if got, err := endpointString(tc.in); err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc.in, got, err)
		}
	}
	if _, err := endpointString(endpoint{}); err == nil {
		t.Fatal("empty endpoint accepted")
	}
}

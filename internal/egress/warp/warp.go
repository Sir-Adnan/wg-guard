// Package warp registers Cloudflare WARP devices for egress outbounds. It
// implements the minimal subset of the Cloudflare client API that the open
// source wgcf client uses (source-reviewed at ViRb3/wgcf de6bba7, API
// v0a5641, 2026-10-12): register a WireGuard public key, bind a WARP+
// license, read the account type.
//
// Cloudflare does not document this API and changes it between app
// releases; registration therefore fails closed with a fixed panel message,
// and operators can always import a wgcf-generated profile as an ordinary
// WireGuard outbound instead.
package warp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress"
)

// Pinned API identity (wgcf de6bba7).
const (
	APIBase       = "https://api.cloudflareclient.com"
	APIVersion    = "v0a5641"
	userAgent     = "1.1.1.1/6.38.9-5641 (Android 16.0.0)"
	clientVersion = "a-6.38.9-5641"
	maxResponse   = 1 << 20
)

// Client is the production registrar.
type Client struct {
	Base string
	HTTP *http.Client
	Now  func() time.Time
}

// New returns a client with an Android-like TLS 1.2 profile (the API rejects
// some non-app handshakes; wgcf pins the same versions, suites and curves).
func New() *Client {
	transport := &http.Transport{
		Proxy:       nil,
		DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
			NextProtos:       []string{"http/1.1"},
		},
		ForceAttemptHTTP2:   false,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{Base: APIBase, HTTP: &http.Client{Transport: transport, Timeout: 30 * time.Second}, Now: time.Now}
}

type endpoint struct {
	Host  string  `json:"host"`
	V4    string  `json:"v4"`
	Ports []int32 `json:"ports"`
}

type account struct {
	AccountType string `json:"account_type"`
	WarpPlus    bool   `json:"warp_plus"`
}

type registerResponse struct {
	ID      string  `json:"id"`
	Token   string  `json:"token"`
	Account account `json:"account"`
	Config  struct {
		Peers []struct {
			PublicKey string   `json:"public_key"`
			Endpoint  endpoint `json:"endpoint"`
		} `json:"peers"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
	} `json:"config"`
}

func (c *Client) do(ctx context.Context, method, path, token string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/"+APIVersion+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("CF-Client-Version", clientVersion)
	req.Header.Set("Connection", "Keep-Alive")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Response bodies are not echoed: they can carry account details.
		return fmt.Errorf("warp: %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("warp: unreadable response")
	}
	return nil
}

// Register creates a WARP device for publicKey.
func (c *Client) Register(ctx context.Context, publicKey string) (egress.WARPRegistration, error) {
	now := c.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	model := "PC"
	body := map[string]any{
		"fcm_token": "", "install_id": "", "key": publicKey, "locale": "en_US",
		"model": model, "tos": now, "serial_number": "", "os_version": "16.0.0",
		"key_type": "curve25519", "tunnel_type": "wireguard",
	}
	var r registerResponse
	if err := c.do(ctx, http.MethodPost, "/reg", "", body, &r); err != nil {
		return egress.WARPRegistration{}, err
	}
	if r.ID == "" || r.Token == "" || len(r.Config.Peers) == 0 || r.Config.Interface.Addresses.V4 == "" {
		return egress.WARPRegistration{}, errors.New("warp: incomplete registration response")
	}
	peer := r.Config.Peers[0]
	ep, err := endpointString(peer.Endpoint)
	if err != nil {
		return egress.WARPRegistration{}, err
	}
	return egress.WARPRegistration{
		DeviceID: r.ID, Token: r.Token, AccountType: r.Account.AccountType, Plus: r.Account.WarpPlus,
		PeerPublicKey: peer.PublicKey, Endpoint: ep, AddressV4: r.Config.Interface.Addresses.V4,
		AddressV6: r.Config.Interface.Addresses.V6,
	}, nil
}

// Addresses reads a registered device's tunnel addresses (GET /reg/{id},
// as wgcf does for an existing device). Devices registered before WG-Guard
// stored the IPv6 address get it from here.
func (c *Client) Addresses(ctx context.Context, deviceID, token string) (v4, v6 string, err error) {
	if deviceID == "" || token == "" {
		return "", "", errors.New("warp: missing device credentials")
	}
	var r registerResponse
	if err := c.do(ctx, http.MethodGet, "/reg/"+deviceID, token, nil, &r); err != nil {
		return "", "", err
	}
	return r.Config.Interface.Addresses.V4, r.Config.Interface.Addresses.V6, nil
}

// endpointString mirrors wgcf's selection: the advertised host, else the v4
// address, with the first advertised port (2408 by default).
func endpointString(e endpoint) (string, error) {
	host := e.Host
	if host == "" {
		host = e.V4
	}
	if host == "" {
		return "", errors.New("warp: no endpoint")
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		if p != "0" && p != "" {
			return net.JoinHostPort(h, p), nil
		}
		host = h
	}
	port := int32(2408)
	if len(e.Ports) > 0 && e.Ports[0] > 0 {
		port = e.Ports[0]
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// ApplyLicense binds a WARP+ license key and returns the resulting account.
func (c *Client) ApplyLicense(ctx context.Context, deviceID, token, license string) (string, bool, error) {
	if deviceID == "" || token == "" {
		return "", false, errors.New("warp: missing device credentials")
	}
	if err := c.do(ctx, http.MethodPut, "/reg/"+deviceID+"/account", token, map[string]string{"license": license}, nil); err != nil {
		return "", false, err
	}
	var a account
	if err := c.do(ctx, http.MethodGet, "/reg/"+deviceID+"/account", token, nil, &a); err != nil {
		return "", false, err
	}
	return a.AccountType, a.WarpPlus, nil
}

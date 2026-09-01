package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/iface"
	webassets "github.com/Sir-Adnan/wg-guard/web"
)

type decodedProfilePreview struct {
	Policy string            `json:"policy"`
	Fields map[string]string `json:"fields"`
}

func previewForm(fields map[string]string, csrf, name, policy string) url.Values {
	form := url.Values{
		"_csrf": {csrf}, "name": {name}, "listen_port": {""}, "subnet": {""},
		"profile_policy": {policy},
	}
	for key, value := range fields {
		if value != "" {
			form.Set(key, value)
		}
	}
	return form
}

func decodeProfilePreview(t *testing.T, recBody string) decodedProfilePreview {
	t.Helper()
	var response decodedProfilePreview
	if err := json.Unmarshal([]byte(recBody), &response); err != nil {
		t.Fatalf("decode preview: %v\n%s", err, recBody)
	}
	return response
}

func TestProfilePreviewRequiresSessionAndCSRF(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()

	if rec := e.post("/interfaces/profile-preview", url.Values{"policy": {"recommended"}}, nil, ""); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous preview = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookie := e.login("owner")
	if rec := e.post("/interfaces/profile-preview", url.Values{"policy": {"recommended"}}, cookie, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("preview without CSRF = %d %s", rec.Code, rec.Body.String())
	}
}

func TestProfilePreviewRecommendedAndRandomized(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	recommendedRec := e.post("/interfaces/profile-preview", url.Values{"policy": {"recommended"}}, cookie, csrf)
	if recommendedRec.Code != http.StatusOK {
		t.Fatalf("recommended preview: %d %s", recommendedRec.Code, recommendedRec.Body.String())
	}
	if recommendedRec.Header().Get("Cache-Control") != "no-store" ||
		recommendedRec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("preview response headers: %v", recommendedRec.Header())
	}
	recommended := decodeProfilePreview(t, recommendedRec.Body.String())
	if recommended.Policy != "recommended" || recommended.Fields["obf_enabled"] != "1" ||
		recommended.Fields["obf_jc"] != strconv.Itoa(iface.RecommendedJc) ||
		recommended.Fields["obf_hpk"] != "" || recommended.Fields["obf_random_trailers"] != "" ||
		recommended.Fields["obf_disable_cookies"] != "" {
		t.Fatalf("recommended preview shape: %+v", recommended)
	}
	for _, key := range []string{"obf_h1", "obf_h2", "obf_h3", "obf_h4"} {
		if strings.Contains(recommended.Fields[key], "-") || recommended.Fields[key] == "" {
			t.Fatalf("recommended %s = %q", key, recommended.Fields[key])
		}
	}

	randomizedRec := e.post("/interfaces/profile-preview", url.Values{"policy": {"randomized"}}, cookie, csrf)
	if randomizedRec.Code != http.StatusOK {
		t.Fatalf("randomized preview: %d %s", randomizedRec.Code, randomizedRec.Body.String())
	}
	randomized := decodeProfilePreview(t, randomizedRec.Body.String())
	if randomized.Policy != "randomized" || !strings.Contains(randomized.Fields["obf_h1"], "-") {
		t.Fatalf("randomized preview shape: %+v", randomized)
	}
	hpk, err := base64.StdEncoding.DecodeString(randomized.Fields["obf_hpk"])
	if err != nil || len(hpk) != 32 {
		t.Fatalf("randomized HPK = %q, %v", randomized.Fields["obf_hpk"], err)
	}
	for _, key := range []string{"obf_s1", "obf_s2", "obf_s3", "obf_s4"} {
		value, err := strconv.Atoi(randomized.Fields[key])
		if err != nil || value < iface.RandomizedSMin {
			t.Fatalf("randomized %s = %q", key, randomized.Fields[key])
		}
	}
	if randomized.Fields["obf_random_trailers"] != "" || randomized.Fields["obf_disable_cookies"] != "" {
		t.Fatalf("unsafe flags enabled: %+v", randomized.Fields)
	}
}

func TestProfilePreviewRejectsInvalidPolicyAndEntropyFailure(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	invalid := e.post("/interfaces/profile-preview", url.Values{"policy": {"custom"}}, cookie, csrf)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy = %d %s", invalid.Code, invalid.Body.String())
	}

	e.srv.ProfileGenerator = func(iface.ProfilePolicy) (iface.Obfuscation, error) {
		return iface.Obfuscation{}, errors.New("entropy source disclosed detail")
	}
	failed := e.post("/interfaces/profile-preview", url.Values{"policy": {"randomized"}}, cookie, csrf)
	if failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("entropy failure = %d %s", failed.Code, failed.Body.String())
	}
	if strings.Contains(failed.Body.String(), "entropy source") || strings.Contains(failed.Body.String(), "obf_hpk") {
		t.Fatalf("entropy failure leaked detail or partial values: %s", failed.Body.String())
	}
}

func TestGeneratedProfileFormPersistsExactPolicy(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)

	for index, policy := range []iface.ProfilePolicy{iface.ProfileRecommended, iface.ProfileRandomized} {
		previewRec := e.post("/interfaces/profile-preview", url.Values{"policy": {string(policy)}}, cookie, csrf)
		if previewRec.Code != http.StatusOK {
			t.Fatalf("%s preview: %d %s", policy, previewRec.Code, previewRec.Body.String())
		}
		preview := decodeProfilePreview(t, previewRec.Body.String())
		name := "awg" + strconv.Itoa(index)
		created := e.post("/interfaces", previewForm(preview.Fields, csrf, name, preview.Policy), cookie, csrf)
		if created.Code != http.StatusSeeOther {
			t.Fatalf("%s form create: %d %s", policy, created.Code, created.Body.String())
		}
		stored, err := e.ifaces.GetByName(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Preset != string(policy) {
			t.Fatalf("%s stored policy = %q", policy, stored.Preset)
		}
		if err := iface.ValidateGeneratedProfile(policy, stored.Obfuscation); err != nil {
			t.Fatalf("%s stored profile: %v", policy, err)
		}
	}

	previewRec := e.post("/interfaces/profile-preview", url.Values{"policy": {"recommended"}}, cookie, csrf)
	preview := decodeProfilePreview(t, previewRec.Body.String())
	tampered := previewForm(preview.Fields, csrf, "awg2", preview.Policy)
	tampered.Set("obf_jc", "5")
	_ = e.post("/interfaces", tampered, cookie, csrf)
	if _, err := e.ifaces.GetByName(t.Context(), "awg2"); err == nil {
		t.Fatal("tampered recommended profile was persisted under the generated policy")
	}
}

func TestGeneratedHPKIsNotRenderedAndIsPreservedOnEdit(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.login("owner")
	csrf := deriveCSRF(cookie.Value)
	created, err := e.ifaces.Create(t.Context(), iface.CreateInput{Name: "awg0", Preset: "randomized"})
	if err != nil {
		t.Fatal(err)
	}
	originalHPK := created.Obfuscation.HeaderProtectionKey
	page := e.get("/interfaces/"+created.ID+"/edit", cookie)
	if page.Code != http.StatusOK {
		t.Fatalf("edit page: %d", page.Code)
	}
	if strings.Contains(page.Body.String(), originalHPK) {
		t.Fatal("stored header protection key rendered into edit HTML")
	}

	fields := profileFormFields(created.Obfuscation)
	delete(fields, "obf_hpk")
	form := previewForm(fields, csrf, "", "randomized")
	form.Set("mtu", strconv.Itoa(created.MTU))
	form.Set("enabled", "1")
	form.Set("endpoint_override", "")
	updated := e.post("/interfaces/"+created.ID+"/edit", form, cookie, csrf)
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("generated edit: %d %s", updated.Code, updated.Body.String())
	}
	stored, err := e.ifaces.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Obfuscation.HeaderProtectionKey != originalHPK || stored.Preset != "randomized" {
		t.Fatalf("generated edit changed HPK/policy: %q %q", stored.Obfuscation.HeaderProtectionKey, stored.Preset)
	}

	clearFields := profileFormFields(stored.Obfuscation)
	delete(clearFields, "obf_hpk")
	clear := previewForm(clearFields, csrf, "", "custom")
	clear.Set("mtu", strconv.Itoa(stored.MTU))
	clear.Set("enabled", "1")
	clear.Set("obf_hpk_clear", "1")
	if rec := e.post("/interfaces/"+created.ID+"/edit", clear, cookie, csrf); rec.Code != http.StatusSeeOther {
		t.Fatalf("clear HPK: %d %s", rec.Code, rec.Body.String())
	}
	stored, err = e.ifaces.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Obfuscation.HeaderProtectionKey != "" || stored.Preset != "custom" {
		t.Fatalf("explicit HPK clear = %q, policy %q", stored.Obfuscation.HeaderProtectionKey, stored.Preset)
	}
}

func TestProfileJavaScriptUsesOnlyServerGeneration(t *testing.T) {
	script, err := webassets.FS.ReadFile("static/js/app.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, forbidden := range []string{"crypto.getRandomValues", "function randInt", "data-randomize-obf"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("browser still owns AWG generation via %q", forbidden)
		}
	}
	for _, required := range []string{"/interfaces/profile-preview", "data-generate-obf", "X-CSRF-Token"} {
		if !strings.Contains(text, required) {
			t.Errorf("server generation flow missing %q", required)
		}
	}
}

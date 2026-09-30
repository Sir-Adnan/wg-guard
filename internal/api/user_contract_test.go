package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Prevent SDK/reference generation from silently omitting fields accepted or
// returned by the user/device contract. Behavior tests cover the semantics.
func TestOpenAPIUserDeviceAndBulkFieldCoverage(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]struct {
							Properties map[string]json.RawMessage `json:"properties"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, wire any, properties map[string]json.RawMessage) {
		t.Helper()
		typ := reflect.TypeOf(wire)
		fields := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			fields[key] = true
			if _, ok := properties[key]; !ok {
				t.Errorf("wire field %s missing from OpenAPI", key)
			}
		}
		for key := range properties {
			if !fields[key] {
				t.Errorf("OpenAPI field %s absent from wire type", key)
			}
		}
	}
	for name, wire := range map[string]any{"UserCreate": userCreateReq{}, "UserPatch": userPatchReq{},
		"User": userDTO{}, "Device": deviceDTO{}, "Template": planDTO{}} {
		t.Run(name, func(t *testing.T) { check(t, wire, doc.Components.Schemas[name].Properties) })
	}
	t.Run("bulk action parameters", func(t *testing.T) {
		params := doc.Paths["/api/v1/users/bulk-action"]["post"].RequestBody.Content["application/json"].Schema.Properties["params"]
		check(t, bulkActionParams{}, params.Properties)
	})
}

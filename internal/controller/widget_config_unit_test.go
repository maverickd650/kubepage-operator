package controller

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidateWidgetConfigsNonObjectConfig covers the "non-object or
// unparseable [config] -> validation error" requirement from issue #104:
// a Config/Options block that decodes to something other than a JSON object
// (here, a JSON array) must be treated as invalid, flipping Available rather
// than being silently skipped or mistaken for an empty config.
func TestValidateWidgetConfigsNonObjectConfig(t *testing.T) {
	instances := []widgetConfigInstance{{
		Location:   `entry "svc" widget[0] (type "cloudflared")`,
		WidgetType: testWidgetTypeCloudflared,
		Raw:        &apiextensionsv1.JSON{Raw: []byte(`["accountId","tunnelId"]`)},
	}}

	availableOverride, configValid := validateWidgetConfigs(instances, 1)

	if availableOverride == nil {
		t.Fatal("validateWidgetConfigs() availableOverride = nil, want non-nil for a non-object config")
	}
	if availableOverride.Status != metav1.ConditionFalse {
		t.Errorf("availableOverride.Status = %v, want False", availableOverride.Status)
	}
	if availableOverride.Reason != reasonInvalidWidgetConfig {
		t.Errorf("availableOverride.Reason = %q, want %q", availableOverride.Reason, reasonInvalidWidgetConfig)
	}
	if got, want := availableOverride.Message, "config is not a JSON object"; !strings.Contains(got, want) {
		t.Errorf("availableOverride.Message = %q, want it to contain %q", got, want)
	}

	if configValid.Status != metav1.ConditionFalse {
		t.Errorf("configValid.Status = %v, want False", configValid.Status)
	}
	if configValid.Reason != reasonInvalidWidgetConfig {
		t.Errorf("configValid.Reason = %q, want %q", configValid.Reason, reasonInvalidWidgetConfig)
	}
}

// TestDecodeConfigKeys table-tests the pure JSON-decoding helper directly:
// nil/empty raw decodes to an empty (valid) key set, a JSON object decodes
// to its keys, and a non-object (array, scalar, malformed) is rejected.
func TestDecodeConfigKeys(t *testing.T) {
	tests := []struct {
		name    string
		raw     *apiextensionsv1.JSON
		wantOK  bool
		wantLen int
	}{
		{name: "nil raw", raw: nil, wantOK: true, wantLen: 0},
		{name: "empty raw", raw: &apiextensionsv1.JSON{}, wantOK: true, wantLen: 0},
		{name: "object", raw: &apiextensionsv1.JSON{Raw: []byte(`{"a":1,"b":2}`)}, wantOK: true, wantLen: 2},
		{name: "array", raw: &apiextensionsv1.JSON{Raw: []byte(`["a","b"]`)}, wantOK: false},
		{name: "scalar", raw: &apiextensionsv1.JSON{Raw: []byte(`5`)}, wantOK: false},
		{name: "malformed", raw: &apiextensionsv1.JSON{Raw: []byte(`{not valid json`)}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, ok := decodeConfigKeys(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("decodeConfigKeys() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && len(keys) != tt.wantLen {
				t.Errorf("decodeConfigKeys() len(keys) = %d, want %d", len(keys), tt.wantLen)
			}
		})
	}
}

package widgetschema

import (
	"slices"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		configKeys  map[string]any
		schema      ConfigSchema
		wantMissing []string
		wantUnknown []string
	}{
		{
			name:       "empty schema, empty config",
			configKeys: map[string]any{},
			schema:     ConfigSchema{},
		},
		{
			name:        "empty schema, unexpected key",
			configKeys:  map[string]any{"foo": "bar"},
			schema:      ConfigSchema{},
			wantUnknown: []string{"foo"},
		},
		{
			name:        "required key missing",
			configKeys:  map[string]any{},
			schema:      ConfigSchema{Required: []string{keyAccountID, keyTunnelID}},
			wantMissing: []string{keyAccountID, keyTunnelID},
		},
		{
			name:       "required keys all present",
			configKeys: map[string]any{keyAccountID: "a", keyTunnelID: "t"},
			schema:     ConfigSchema{Required: []string{keyAccountID, keyTunnelID}},
		},
		{
			name:        "one required key missing, one present",
			configKeys:  map[string]any{keyAccountID: "a"},
			schema:      ConfigSchema{Required: []string{keyAccountID, keyTunnelID}},
			wantMissing: []string{keyTunnelID},
		},
		{
			name:        "optional key present is not unknown",
			configKeys:  map[string]any{keyLabel: "custom"},
			schema:      ConfigSchema{Required: []string{keyQuery}, Optional: []string{keyLabel}},
			wantMissing: []string{keyQuery},
		},
		{
			name:        "unrecognized key alongside satisfied required key",
			configKeys:  map[string]any{keyQuery: "up", "labell": "typo"},
			schema:      ConfigSchema{Required: []string{keyQuery}, Optional: []string{keyLabel}},
			wantUnknown: []string{"labell"},
		},
		{
			name:        "missing required and unknown key both reported",
			configKeys:  map[string]any{"acountId": "typo"},
			schema:      ConfigSchema{Required: []string{keyAccountID, keyTunnelID}},
			wantMissing: []string{keyAccountID, keyTunnelID},
			wantUnknown: []string{"acountId"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing, unknown := ValidateConfig(tt.configKeys, tt.schema)
			if !slices.Equal(missing, tt.wantMissing) {
				t.Errorf("ValidateConfig() missing = %v, want %v", missing, tt.wantMissing)
			}
			if !slices.Equal(unknown, tt.wantUnknown) {
				t.Errorf("ValidateConfig() unknown = %v, want %v", unknown, tt.wantUnknown)
			}
		})
	}
}

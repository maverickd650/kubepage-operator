package dashboard

import (
	"maps"
	"testing"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// testSecretFieldKey is a generic secrets-map key reused across most of this
// file's test cases, distinct from testSecretField ("token", used by the
// poller's end-to-end secret-resolution tests in poller_test.go).
const testSecretFieldKey = "key"

func ptrSVS(value string) *pagev1alpha1.SecretValueSource {
	return &pagev1alpha1.SecretValueSource{Value: &value}
}

func TestMergeWidgetSecrets(t *testing.T) {
	tests := []struct {
		name       string
		widgetType string
		secrets    map[string]pagev1alpha1.SecretValueSource
		caCert     *pagev1alpha1.SecretValueSource
		defaults   map[string]pagev1alpha1.WidgetDefaultsEntry
		wantSecret map[string]pagev1alpha1.SecretValueSource
		wantCACert *pagev1alpha1.SecretValueSource
	}{
		{
			name:       "no defaults leaves widget unchanged",
			widgetType: widgetTypeOpenWeatherMap,
			secrets:    map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
			defaults:   nil,
			wantSecret: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
		},
		{
			name:       "no default entry for this widget type leaves widget unchanged",
			widgetType: widgetTypeOpenWeatherMap,
			secrets:    map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testGrafanaIconSlug: {Secrets: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("other-type")}},
			},
			wantSecret: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
		},
		{
			name:       "default fills a gap the widget doesn't set",
			widgetType: widgetTypeOpenWeatherMap,
			secrets:    nil,
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				widgetTypeOpenWeatherMap: {Secrets: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("default")}},
			},
			wantSecret: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("default")},
		},
		{
			name:       "widget's own secret wins over the default for the same key",
			widgetType: widgetTypeOpenWeatherMap,
			secrets:    map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				widgetTypeOpenWeatherMap: {Secrets: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("default")}},
			},
			wantSecret: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("own")},
		},
		{
			name:       "default and own secrets merge on different keys",
			widgetType: widgetTypeOpenWeatherMap,
			secrets:    map[string]pagev1alpha1.SecretValueSource{"username": *ptrSVS("own-user")},
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				widgetTypeOpenWeatherMap: {Secrets: map[string]pagev1alpha1.SecretValueSource{testSecretFieldKey: *ptrSVS("default-key")}},
			},
			wantSecret: map[string]pagev1alpha1.SecretValueSource{
				"username":         *ptrSVS("own-user"),
				testSecretFieldKey: *ptrSVS("default-key"),
			},
		},
		{
			name:       "default caCert fills gap when widget has none",
			widgetType: testGrafanaIconSlug,
			caCert:     nil,
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testGrafanaIconSlug: {CACert: ptrSVS("default-ca")},
			},
			wantCACert: ptrSVS("default-ca"),
		},
		{
			name:       "widget's own caCert wins over the default",
			widgetType: testGrafanaIconSlug,
			caCert:     ptrSVS("own-ca"),
			defaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testGrafanaIconSlug: {CACert: ptrSVS("default-ca")},
			},
			wantCACert: ptrSVS("own-ca"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSecrets, gotCACert := mergeWidgetSecrets(tt.widgetType, tt.secrets, tt.caCert, tt.defaults)

			if len(gotSecrets) != len(tt.wantSecret) || !maps.EqualFunc(gotSecrets, tt.wantSecret, func(a, b pagev1alpha1.SecretValueSource) bool {
				return ptrStringEqual(a.Value, b.Value)
			}) {
				t.Errorf("secrets = %#v, want %#v", gotSecrets, tt.wantSecret)
			}

			switch {
			case gotCACert == nil && tt.wantCACert == nil:
			case gotCACert == nil || tt.wantCACert == nil:
				t.Errorf("caCert = %#v, want %#v", gotCACert, tt.wantCACert)
			case !ptrStringEqual(gotCACert.Value, tt.wantCACert.Value):
				t.Errorf("caCert = %#v, want %#v", gotCACert, tt.wantCACert)
			}
		})
	}
}

func ptrStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

package render

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const testInstanceName = "instance-sample"

func ptr[T any](v T) *T { return &v }

func TestSettings_Minimal(t *testing.T) {
	got, err := Settings(&pagev1alpha1.ConfigurationSpec{
		InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
		Title:       ptr("My Homepage"),
	})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	assertGolden(t, "settings_minimal", got)
}

func TestSettings_TypedFields(t *testing.T) {
	got, err := Settings(&pagev1alpha1.ConfigurationSpec{
		InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
		Title:       ptr("My Homepage"),
		Description: ptr("A description of my awesome homepage"),
		StartUrl:    ptr("/"),
		Theme:       ptr("dark"),
		Color:       ptr("slate"),
		HeaderStyle: ptr("boxed"),
		Language:    ptr("en"),
		Target:      ptr("_blank"),
		FullWidth:   ptr(true),
		HideVersion: ptr(true),
		Favicon:     ptr("/images/favicon.ico"),
		CardBlur:    ptr("xs"),
		Background: &pagev1alpha1.BackgroundSpec{
			Image:      ptr("/images/background.png"),
			Blur:       ptr("sm"),
			Saturate:   ptr(int32(50)),
			Brightness: ptr(int32(50)),
			Opacity:    ptr(int32(50)),
		},
	})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	assertGolden(t, "settings_typed_fields", got)
}

// TestSettings_ExtraPassthroughAndPrecedence covers two things in the Extra
// passthrough: a key with no typed equivalent (providers) passes through
// untouched, and a key that collides with a typed field (title) is overridden
// by the typed field rather than the Extra value.
func TestSettings_ExtraPassthroughAndPrecedence(t *testing.T) {
	got, err := Settings(&pagev1alpha1.ConfigurationSpec{
		InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
		Title:       ptr("Typed Title Wins"),
		Extra: &apiextensionsv1.JSON{
			Raw: []byte(`{"title":"Extra Title Loses","providers":{"openweathermap":"apikey123"},"quicklaunch":{"hideVisitURL":true}}`),
		},
	})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	assertGolden(t, "settings_extra_passthrough", got)
}

func TestKubernetesDisabled(t *testing.T) {
	got, err := KubernetesDisabled()
	if err != nil {
		t.Fatalf("KubernetesDisabled: %v", err)
	}
	assertGolden(t, "kubernetes_disabled", got)
}

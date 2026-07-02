package controller

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/dashboard"
)

// serviceEntryWidgetTypes and infoWidgetPollableTypes must be kept identical
// to the two CEL allow-lists in config/admission/widget_type_policy.yaml.
// TestRegisteredWidgetTypesCoveredByPolicy below fails if a type registered
// in internal/dashboard is missing from both lists, which is the drift the
// YAML's own comment promises but the envtest specs in this file (which only
// probe a handful of fixed type strings) don't actually catch for a type
// *addition*.
var (
	serviceEntryWidgetTypes = []string{
		"plex", "stash", "paperlessngx", testWidgetTypeGrafana, testWidgetTypePrometheus,
		"prometheusmetric", "unifi", "truenas", "cloudflared", "linkwarden",
		"homeassistant", "mealie", "customapi",
	}
	// infoWidgetPollableTypes is the subset of config/admission's infowidget-type
	// allow-list that's also a registered dashboard.Widget; "greeting" and
	// "datetime" are rendered statically by internal/dashboard/server.go and
	// never go through Register, so they're intentionally excluded here.
	infoWidgetPollableTypes = []string{testWidgetTypeOpenMeteo, "kubemetrics"}
)

// TestRegisteredWidgetTypesCoveredByPolicy guards against a widget added to
// internal/dashboard (via Register in some widget's init()) being forgotten
// in config/admission/widget_type_policy.yaml's CEL allow-lists: every
// registered type must appear in at least one of the two lists above, or a
// valid, working widget type would be rejected at admission time.
func TestRegisteredWidgetTypesCoveredByPolicy(t *testing.T) {
	allowed := slices.Concat(serviceEntryWidgetTypes, infoWidgetPollableTypes)
	for _, widgetType := range dashboard.RegisteredTypes() {
		if !slices.Contains(allowed, widgetType) {
			t.Errorf("dashboard widget type %q is registered but missing from both serviceEntryWidgetTypes and infoWidgetPollableTypes; add it to config/admission/widget_type_policy.yaml and to one of these lists", widgetType)
		}
	}
}

// These specs verify the shipped widget-type ValidatingAdmissionPolicies
// (config/admission/widget_type_policy.yaml) reject unknown widget types,
// while admitting the supported ones. Loading the real manifest (rather than
// reconstructing the CEL in Go) means the allowed lists drifting from the
// internal/dashboard registry — e.g. a newly-registered widget left out of the
// policy — fails the build: the policy with failurePolicy: Fail would reject
// the valid fixture.
var _ = Describe("Widget-type ValidatingAdmissionPolicies", Ordered, func() {
	BeforeAll(func() {
		applyManifest(filepath.Join("..", "..", "config", "admission", "widget_type_policy.yaml"))

		// Policy enforcement isn't instantaneous after the policy/binding are
		// created; poll a known-invalid object until it's rejected before
		// asserting individual cases, so the specs don't race the apiserver.
		Eventually(func() bool {
			se := serviceEntryWithWidgetType("warmup", "does-not-exist")
			err := k8sClient.Create(ctx, se)
			if err == nil {
				_ = k8sClient.Delete(ctx, se)
				return false
			}
			return apierrors.IsInvalid(err) || apierrors.IsForbidden(err)
		}, 30*time.Second, time.Second).Should(BeTrue(), "policy should begin rejecting unknown widget types")
	})

	Describe("ServiceEntry widget type", func() {
		It("rejects an unknown widget type", func() {
			se := serviceEntryWithWidgetType("se-bad-type", "plexx")
			Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
		})

		It("admits a supported service widget type", func() {
			se := serviceEntryWithWidgetType("se-good-type", testWidgetTypeGrafana)
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})

		It("admits a ServiceEntry with no widgets", func() {
			se := serviceEntryWithWidgetType("se-no-widgets", "")
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})

		It("rejects a header-only widget type used as a service card", func() {
			// kubemetrics is a cluster-only header widget; its Poll errors, so it
			// must not be accepted as a ServiceEntry card.
			se := serviceEntryWithWidgetType("se-header-type", "kubemetrics")
			Expect(k8sClient.Create(ctx, se)).NotTo(Succeed())
		})
	})

	Describe("InfoWidget type", func() {
		It("rejects an unknown type", func() {
			iw := infoWidgetWithType("iw-bad-type", "weatherz")
			Expect(k8sClient.Create(ctx, iw)).NotTo(Succeed())
		})

		It("admits a supported header type", func() {
			iw := infoWidgetWithType("iw-good-type", "datetime")
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})

		It("admits the static logo type", func() {
			iw := infoWidgetWithType("iw-logo-type", "logo")
			Expect(k8sClient.Create(ctx, iw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, iw)).To(Succeed())
		})
	})
})

// serviceEntryWithWidgetType builds a minimally-valid ServiceEntry with a
// single widget of the given type; an empty widgetType means no widgets.
func serviceEntryWithWidgetType(name, widgetType string) *pagev1alpha1.ServiceEntry {
	se := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: policyInstanceRef},
			Group:       policyTestGroup,
			Name:        name,
		},
	}
	if widgetType != "" {
		se.Spec.Widgets = []pagev1alpha1.ServiceWidget{{Type: widgetType}}
	}
	return se
}

// infoWidgetWithType builds a minimally-valid InfoWidget of the given type.
func infoWidgetWithType(name, widgetType string) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: policyInstanceRef},
			Type:        widgetType,
		},
	}
}

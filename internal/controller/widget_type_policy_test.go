package controller

import (
	"slices"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/dashboard"
)

// serviceEntryWidgetTypes and infoWidgetPollableTypes must be kept identical
// to the two Enum allow-lists on ServiceWidget.Type/InfoWidgetSpec.Type
// (api/v1alpha1/servicecard_types.go, api/v1alpha1/infowidget_types.go).
// TestRegisteredWidgetTypesCoveredByPolicy below fails if a type registered
// in internal/dashboard is missing from both lists, which is the drift the
// enum markers' own comment promises but the envtest specs in this file
// (which only probe a handful of fixed type strings) don't actually catch
// for a type *addition*.
var (
	serviceEntryWidgetTypes = []string{
		testWidgetTypePlex, "stash", "paperlessngx", testWidgetTypeGrafana, testWidgetTypePrometheus,
		"prometheusmetric", "unifi", "truenas", testWidgetTypeCloudflared, "linkwarden",
		"homeassistant", "mealie", "customapi", "iframe",
		"sonarr", "radarr", "jellyfin", "jellyseerr", "immich", "adguard",
		"pihole", "uptime-kuma", "portainer", "argocd", "gitea", "tautulli",
		"proxmox", "nextcloud", "opnsense", "netdata", "speedtest", "gatus",
	}
	// infoWidgetPollableTypes is the subset of InfoWidgetSpec.Type's Enum
	// allow-list that's also a registered dashboard.Widget; "greeting" and
	// "datetime" are rendered statically by internal/dashboard/server.go and
	// never go through Register, so they're intentionally excluded here.
	infoWidgetPollableTypes = []string{testWidgetTypeOpenMeteo, "kubemetrics", "glances", "longhorn", "openweathermap"}
)

// TestRegisteredWidgetTypesCoveredByPolicy guards against a widget added to
// internal/dashboard (via Register in some widget's init()) being forgotten
// in the ServiceWidget.Type/InfoWidgetSpec.Type Enum markers: every
// registered type must appear in at least one of the two lists above, or a
// valid, working widget type would be rejected at admission time.
func TestRegisteredWidgetTypesCoveredByPolicy(t *testing.T) {
	allowed := slices.Concat(serviceEntryWidgetTypes, infoWidgetPollableTypes)
	for _, widgetType := range dashboard.RegisteredTypes() {
		if !slices.Contains(allowed, widgetType) {
			t.Errorf("dashboard widget type %q is registered but missing from both serviceEntryWidgetTypes and infoWidgetPollableTypes; add it to the Enum markers on ServiceWidget.Type/InfoWidgetSpec.Type and to one of these lists", widgetType)
		}
	}
}

// These specs verify the ServiceWidget.Type/InfoWidgetSpec.Type CRD schema
// Enum markers reject unknown widget types, while admitting the supported
// ones.
var _ = Describe("Widget-type CRD schema validation", func() {
	Describe("ServiceCard widget type", func() {
		It("rejects an unknown widget type", func() {
			se := serviceEntryWithWidgetType("se-bad-type", "plexx")
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})

		It("admits a supported service widget type", func() {
			se := serviceEntryWithWidgetType("se-good-type", testWidgetTypeGrafana)
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})

		It("admits a ServiceCard with no widgets", func() {
			se := serviceEntryWithWidgetType("se-no-widgets", "")
			Expect(k8sClient.Create(ctx, se)).To(Succeed())
			Expect(k8sClient.Delete(ctx, se)).To(Succeed())
		})

		It("rejects a header-only widget type used as a service card", func() {
			// kubemetrics is a cluster-only header widget; its Poll errors, so it
			// must not be accepted as a ServiceCard card.
			se := serviceEntryWithWidgetType("se-header-type", "kubemetrics")
			err := k8sClient.Create(ctx, se)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})
	})

	Describe("InfoWidget type", func() {
		It("rejects an unknown type", func() {
			iw := infoWidgetWithType("iw-bad-type", "weatherz")
			err := k8sClient.Create(ctx, iw)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
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

// serviceEntryWithWidgetType builds a minimally-valid ServiceCard whose
// single services entry has a single widget of the given type; an empty
// widgetType means no widgets.
func serviceEntryWithWidgetType(name, widgetType string) *pagev1alpha1.ServiceCard {
	entry := pagev1alpha1.ServiceEntry{Name: name}
	if widgetType != "" {
		entry.Widgets = []pagev1alpha1.ServiceWidget{{Type: widgetType}}
	}
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Group:        policyTestGroup,
			Services:     []pagev1alpha1.ServiceEntry{entry},
		},
	}
}

// infoWidgetWithType builds a minimally-valid InfoWidget whose single entry
// is of the given type.
func infoWidgetWithType(name, widgetType string) *pagev1alpha1.InfoWidget {
	return &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: policyTestNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: policyDashboardRef},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{Type: widgetType},
			},
		},
	}
}

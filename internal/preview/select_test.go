package preview

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func newDashboard(namespace, name string) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
}

func TestSelectDashboardSingleAutoSelects(t *testing.T) {
	d := newDashboard("", "sample")
	got, err := selectDashboard([]client.Object{d}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != d {
		t.Errorf("selectDashboard returned a different object than the sole candidate")
	}
}

func TestSelectDashboardNoneFound(t *testing.T) {
	_, err := selectDashboard(nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "no Dashboard object found") {
		t.Fatalf("err = %v, want a %q error", err, "no Dashboard object found")
	}
}

func TestSelectDashboardMultipleRequiresDisambiguation(t *testing.T) {
	a := newDashboard("ns-a", "dash")
	b := newDashboard("ns-b", "dash")
	_, err := selectDashboard([]client.Object{a, b}, "", "")
	if err == nil || !strings.Contains(err.Error(), "multiple Dashboards found") {
		t.Fatalf("err = %v, want a %q error", err, "multiple Dashboards found")
	}
	if !strings.Contains(err.Error(), "--namespace") {
		t.Errorf("err = %v, want it to mention --namespace: these candidates set distinct namespaces, so it can help", err)
	}
}

// TestSelectDashboardMultipleNamespaceLessDoesNotSuggestNamespaceFlag covers
// the case --namespace structurally can't help with: two candidates that
// both leave metadata.namespace unset are treated as matching any
// --namespace value (see selectDashboard's doc comment), so no value passed
// to --namespace would ever narrow them down. The error should say so
// instead of suggesting a flag that can't work.
func TestSelectDashboardMultipleNamespaceLessDoesNotSuggestNamespaceFlag(t *testing.T) {
	a := newDashboard("", "dash-a")
	b := newDashboard("", "dash-b")
	_, err := selectDashboard([]client.Object{a, b}, "some-namespace", "")
	if err == nil || !strings.Contains(err.Error(), "multiple Dashboards found") {
		t.Fatalf("err = %v, want a %q error", err, "multiple Dashboards found")
	}
	// The message may still mention "--namespace" while explaining that it
	// won't help (see the actual hint text); what it must NOT do is repeat
	// the generic "--namespace/--dashboard-name to select one" phrasing that
	// implies passing --namespace is a viable fix here.
	if strings.Contains(err.Error(), "--namespace/--dashboard-name") {
		t.Errorf("err = %v, should not offer --namespace as a fix: none of these candidates set metadata.namespace", err)
	}
	if !strings.Contains(err.Error(), "--dashboard-name") {
		t.Errorf("err = %v, want it to suggest --dashboard-name", err)
	}
}

func TestSelectDashboardDisambiguatedByNamespace(t *testing.T) {
	a := newDashboard("ns-a", "dash")
	b := newDashboard("ns-b", "dash")
	got, err := selectDashboard([]client.Object{a, b}, "ns-b", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Errorf("selectDashboard picked %v, want the ns-b Dashboard", got)
	}
}

func TestSelectDashboardDisambiguatedByName(t *testing.T) {
	a := newDashboard("ns", "one")
	b := newDashboard("ns", "two")
	got, err := selectDashboard([]client.Object{a, b}, "", "two")
	if err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Errorf("selectDashboard picked %v, want %q", got, "two")
	}
}

func TestSelectDashboardEmptyNamespaceMatchesAnyNamespaceFilter(t *testing.T) {
	// A Dashboard with no namespace set (e.g. hand-written sample YAML) is
	// still a valid --namespace=foo candidate, since applyDefaultNamespace
	// will resolve it to foo afterward.
	d := newDashboard("", "sample")
	got, err := selectDashboard([]client.Object{d}, "foo", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != d {
		t.Errorf("selectDashboard should have matched the namespace-less Dashboard")
	}
}

func TestApplyDefaultNamespaceFallsBackToPreview(t *testing.T) {
	d := newDashboard("", "sample")
	bookmark := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Name: "gh"}}
	explicit := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "keep"}}

	applyDefaultNamespace([]client.Object{d, bookmark, explicit}, d)

	if d.Namespace != defaultNamespace {
		t.Errorf("Dashboard namespace = %q, want %q", d.Namespace, defaultNamespace)
	}
	if bookmark.Namespace != defaultNamespace {
		t.Errorf("Bookmark namespace = %q, want %q", bookmark.Namespace, defaultNamespace)
	}
	if explicit.Namespace != "other" {
		t.Errorf("explicitly-namespaced object was overwritten: got %q, want %q", explicit.Namespace, "other")
	}
}

func TestApplyDefaultNamespaceUsesDashboardNamespace(t *testing.T) {
	d := newDashboard("prod", "sample")
	bookmark := &pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Name: "gh"}}

	applyDefaultNamespace([]client.Object{d, bookmark}, d)

	if bookmark.Namespace != "prod" {
		t.Errorf("Bookmark namespace = %q, want %q", bookmark.Namespace, "prod")
	}
}

func TestLoadEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dashboard.yaml", dashboardYAML)
	writeFile(t, dir, "bookmark.yaml", `apiVersion: page.kubepage.dev/v1alpha1
kind: Bookmark
metadata:
  name: gh
spec:
  dashboardRef:
    name: sample
  group: Dev
  name: Github
  href: https://github.com/
`)
	writeFile(t, dir, "secret.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: creds
stringData:
  token: hunter2
`)

	result, err := Load(Config{Scheme: testScheme(t), Paths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Namespace != defaultNamespace {
		t.Errorf("Namespace = %q, want %q", result.Namespace, defaultNamespace)
	}
	if result.DashboardName != "sample" {
		t.Errorf("DashboardName = %q, want %q", result.DashboardName, "sample")
	}

	var bm pagev1alpha1.Bookmark
	if err := result.Reader.Get(context.Background(),
		types.NamespacedName{Namespace: defaultNamespace, Name: "gh"}, &bm); err != nil {
		t.Errorf("getting Bookmark through Result.Reader: %v", err)
	}
}

func TestLoadNoFilesFound(t *testing.T) {
	_, err := Load(Config{Scheme: testScheme(t), Paths: []string{t.TempDir()}})
	if err == nil || !strings.Contains(err.Error(), "no .yaml/.yml files found") {
		t.Fatalf("err = %v, want a %q error", err, "no .yaml/.yml files found")
	}
}

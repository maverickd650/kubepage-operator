package dashboard

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var nonceRe = regexp.MustCompile(`nonce-[A-Za-z0-9+/=]+`)

func normalizeHTML(s string) string {
	return nonceRe.ReplaceAllString(s, "nonce-NORMALIZED")
}

func goldenTest(t *testing.T, name string, handler http.Handler, path string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	got := normalizeHTML(rec.Body.String())
	goldenPath := filepath.Join("testdata", name+".golden.html")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run with UPDATE_GOLDEN=1 to create)", goldenPath, err)
	}

	if !bytes.Equal([]byte(got), want) {
		diff := diffFirstMismatch(string(want), got)
		t.Errorf("output differs from golden file %s (run with UPDATE_GOLDEN=1 to update):\n%s", goldenPath, diff)
	}
}

func diffFirstMismatch(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var buf strings.Builder
	maxLines := max(len(gotLines), len(wantLines))

	shown := 0
	for i := range maxLines {
		if shown >= 10 {
			buf.WriteString("... (more differences)\n")
			break
		}
		var wl, gl string
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if wl != gl {
			fmt.Fprintf(&buf, "--- want line [%d]: %s\n+++ got  line [%d]: %s\n", i+1, wl, i+1, gl)
			shown++
		}
	}
	if len(wantLines) != len(gotLines) {
		fmt.Fprintf(&buf, "line count: want %d, got %d\n", len(wantLines), len(gotLines))
	}
	return buf.String()
}

func TestGoldenFragment(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/grafana/0", Group: testGroup, ServiceName: "Grafana",
		Fields: []Field{
			{Label: labelStatus, Value: statusHealthy, Highlight: HighlightGood},
			{Label: "Version", Value: testGrafanaVersion},
		},
	})
	store.Set(Card{
		Key: "ns/plex/0", Group: testGroupMedia, ServiceName: testMultiEntryNamePlex,
		Fields: []Field{
			{Label: "Streams", Value: "3"},
		},
	})
	store.Set(Card{
		Key: "ns/broken/0", Group: testGroup, ServiceName: testBrokenServiceName,
		Err: testUnreachableErr,
	})

	style := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(style).Build()
	srv := &Server{Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	goldenTest(t, "fragment_populated", srv.Routes(), "/fragment")
}

func TestGoldenFragmentEmpty(t *testing.T) {
	store := NewStore()
	style := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(style).Build()
	srv := &Server{Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	goldenTest(t, "fragment_empty", srv.Routes(), "/fragment")
}

func TestGoldenHeader(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/clock/0", Header: true, ServiceName: testClockName,
		Fields: []Field{{Label: "Time", Value: "12:00"}},
	})
	store.Set(Card{
		Key: "ns/greet/0", Header: true, ServiceName: testGreetName,
		Fields: []Field{{Label: "Greeting", Value: "Good afternoon"}},
	})

	objs := []client.Object{
		&pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Widgets: []pagev1alpha1.InfoWidgetEntry{{
					Type: "datetime",
				}},
			},
		},
		&pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: testGreetName, Namespace: testNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Widgets: []pagev1alpha1.InfoWidgetEntry{{
					Type: "greeting",
				}},
			},
		},
		&pagev1alpha1.Dashboard{
			ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
			Spec: pagev1alpha1.DashboardSpec{
				Style: &pagev1alpha1.StyleSpec{},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	srv := &Server{Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	goldenTest(t, "header_widgets", srv.Routes(), "/header")
}

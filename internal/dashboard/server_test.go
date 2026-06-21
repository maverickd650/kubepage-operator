package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func newTestServer(t *testing.T, store *Store, objs ...client.Object) *Server {
	t.Helper()
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &Server{Store: store, Reader: cl, Namespace: testNamespace, InstanceName: testInstanceName}
}

func TestServerFragmentRendersCards(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/prom/0", Group: testGroup, ServiceName: testServiceName,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}},
	})
	store.Set(Card{
		Key: "ns/broken/0", Group: testGroup, ServiceName: "Broken",
		Err: "unreachable",
	})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Monitoring", "Prometheus", "Healthy", "Broken", "unreachable"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersBookmarks(t *testing.T) {
	bookmark := &pagev1alpha1.Bookmark{
		ObjectMeta: metav1.ObjectMeta{Name: "docs", Namespace: testNamespace},
		Spec: pagev1alpha1.BookmarkSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testBookmarkGroup,
			Name:        "Docs",
			Href:        "https://example.invalid/docs",
		},
	}
	srv := newTestServer(t, NewStore(), bookmark)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{testBookmarkGroup, "Docs", "https://example.invalid/docs"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerIndexServesShell(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/fragment") {
		t.Errorf("index body should hx-get /fragment:\n%s", rec.Body.String())
	}
}

func TestServerIndexAppliesConfigurationTheme(t *testing.T) {
	theme := "light"
	color := "blue"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Theme:       &theme,
			Color:       &color,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`data-theme="light"`, AccentHex("blue")} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

func TestGroupCardsPreservesOrder(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "A", ServiceName: "a2"},
		{Group: "B", ServiceName: "b1"},
	}
	groups := groupCards(cards)
	if len(groups) != 2 || groups[0].Name != "A" || len(groups[0].Cards) != 2 || groups[1].Name != "B" {
		t.Fatalf("groupCards() = %+v", groups)
	}
}

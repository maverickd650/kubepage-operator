package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

func TestServerMetricsRoute(t *testing.T) {
	srv := newTestServer(t, NewStore())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("/metrics body missing expected Prometheus Go-runtime metric:\n%s", rec.Body.String())
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
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
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

func TestServerIndexAppliesLookFields(t *testing.T) {
	title := "My Lab"
	favicon := "https://example.invalid/fav.ico"
	cardBlur := "lg"
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Title:       &title,
			Favicon:     &favicon,
			CardBlur:    &cardBlur,
		},
	}
	srv := newTestServer(t, NewStore(), cfg)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"<title>My Lab</title>", favicon, "--card-blur: 16px", `hx-get="/header"`} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q:\n%s", want, body)
		}
	}
}

func TestServerFragmentRendersMonitorAndTarget(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/svc/0", Group: testGroup, ServiceName: "Svc",
		Href: "https://svc.invalid", Target: "_self",
		Status: "Up", StatusStyle: testStatusBasic, Latency: "5ms",
		ShowStats: true,
	})

	srv := newTestServer(t, store)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`target="_self"`, "Up", "5ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

func TestServerHeaderRendersWidgets(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "header/weather", ServiceName: testHeaderWeather, Header: true,
		Fields: []Field{{Label: labelWeather, Value: "10°C"}, {Label: labelConditions, Value: condClear}},
	})

	greeting := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeGreeting,
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"text":"Welcome"}`)},
		},
	}
	weather := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        "openmeteo",
		},
	}
	srv := newTestServer(t, store, greeting, weather)
	req := httptest.NewRequest(http.MethodGet, "/header", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Welcome", "10°C", condClear} {
		if !strings.Contains(body, want) {
			t.Errorf("header body missing %q:\n%s", want, body)
		}
	}
}

func TestServiceCardsFiltersHeaderCards(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: testSvcName, Header: false},
		{ServiceName: testHeaderWeather, Header: true},
	}
	got := serviceCards(cards)
	if len(got) != 1 || got[0].ServiceName != testSvcName {
		t.Errorf("serviceCards() = %+v, want only the non-header card", got)
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

func TestLayoutTabsNoLayoutReturnsSingleUnnamedTab(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	tabs := layoutTabs(cards, nil)
	if len(tabs) != 1 || tabs[0].Name != "" || len(tabs[0].Groups) != 2 {
		t.Fatalf("layoutTabs() with no layout = %+v, want one unnamed tab with both groups", tabs)
	}
}

func TestLayoutTabsArrangesGroupsAndStyles(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	cols := int32(3)
	layout := []LayoutTab{
		{Name: testTab1, Groups: []LayoutGroup{{Name: "A", Columns: &cols, Style: testStyleRow, IconURL: "https://icon.invalid/a.png"}}},
		{Name: testTab2, Groups: []LayoutGroup{{Name: "B"}}},
	}
	tabs := layoutTabs(cards, layout)
	if len(tabs) != 2 {
		t.Fatalf("layoutTabs() = %+v, want 2 tabs", tabs)
	}
	if tabs[0].Name != testTab1 || len(tabs[0].Groups) != 1 || tabs[0].Groups[0].Name != "A" {
		t.Fatalf("tabs[0] = %+v", tabs[0])
	}
	g := tabs[0].Groups[0]
	if g.Columns == nil || *g.Columns != cols || g.Style != testStyleRow || g.IconURL != "https://icon.invalid/a.png" {
		t.Errorf("tabs[0].Groups[0] style = %+v, want columns=3 style=row iconURL set", g)
	}
	if tabs[1].Name != testTab2 || len(tabs[1].Groups) != 1 || tabs[1].Groups[0].Name != "B" {
		t.Fatalf("tabs[1] = %+v", tabs[1])
	}
}

func TestLayoutTabsAppendsUnreferencedGroupsToOther(t *testing.T) {
	cards := []Card{
		{Group: "A", ServiceName: "a1"},
		{Group: "B", ServiceName: "b1"},
	}
	layout := []LayoutTab{{Name: testTab1, Groups: []LayoutGroup{{Name: "A"}}}}
	tabs := layoutTabs(cards, layout)
	if len(tabs) != 2 || tabs[1].Name != testOtherGroup || len(tabs[1].Groups) != 1 || tabs[1].Groups[0].Name != "B" {
		t.Fatalf("layoutTabs() = %+v, want Group B appended to a trailing \"Other\" tab", tabs)
	}
}

func TestLayoutTabsGroupReferencedTwiceUsesFirstTab(t *testing.T) {
	cards := []Card{{Group: "A", ServiceName: "a1"}}
	layout := []LayoutTab{
		{Name: testTab1, Groups: []LayoutGroup{{Name: "A"}}},
		{Name: testTab2, Groups: []LayoutGroup{{Name: "A"}}},
	}
	tabs := layoutTabs(cards, layout)
	if len(tabs) != 2 || len(tabs[0].Groups) != 1 || len(tabs[1].Groups) != 0 {
		t.Fatalf("layoutTabs() = %+v, want Group A only under Tab1", tabs)
	}
}

func TestServerFragmentRendersTabs(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/a/0", Group: "Infra", ServiceName: "Svc A"})
	store.Set(Card{Key: "ns/b/0", Group: "Apps", ServiceName: "Svc B"})

	cols := int32(2)
	cfg := &pagev1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{Name: testCfgName, Namespace: testNamespace},
		Spec: pagev1alpha1.ConfigurationSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Name: testInfraTab, Groups: []pagev1alpha1.LayoutGroupSpec{{Name: "Infra", Columns: &cols}}},
			},
		},
	}
	srv := newTestServer(t, store, cfg)
	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{testInfraTab, testOtherGroup, "Svc A", "Svc B", "tab-btn"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment body missing %q:\n%s", want, body)
		}
	}
}

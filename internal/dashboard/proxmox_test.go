package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const (
	proxmoxTestUsername = "root@pam!homepage"
	proxmoxTestPassword = "secret-token-value"
)

func TestProxmoxWidgetPoll(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[
			{"type":"qemu","status":"running","node":"pve1","template":0},
			{"type":"qemu","status":"stopped","node":"pve1","template":0},
			{"type":"qemu","status":"running","node":"pve1","template":1},
			{"type":"lxc","status":"running","node":"pve1","template":0},
			{"type":"node","status":"online","node":"pve1","maxmem":8000000000,"mem":4000000000,"maxcpu":4,"cpu":0.5}
		]}`))
	}))
	defer srv.Close()

	got, err := (proxmoxWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: proxmoxTestUsername, secretPassword: proxmoxTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	cpuPct, memPct := 50, 50
	want := []Field{
		{Label: labelVMs, Value: "1 / 2"},
		{Label: labelLXC, Value: testCount11},
		{Label: labelCPU, Value: testPct50, Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: testPct50, Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	wantAuth := "PVEAPIToken=" + proxmoxTestUsername + "=" + proxmoxTestPassword
	if gotAuth != wantAuth {
		t.Errorf("Authorization header = %q, want %q", gotAuth, wantAuth)
	}
}

func TestProxmoxWidgetPollFiltersByNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[
			{"type":"qemu","status":"running","node":"pve1","template":0},
			{"type":"qemu","status":"running","node":"pve2","template":0}
		]}`))
	}))
	defer srv.Close()

	got, err := (proxmoxWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: proxmoxTestUsername, secretPassword: proxmoxTestPassword},
		Config:  []byte(`{"node":"pve1"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelVMs, Value: testCount11},
		{Label: labelLXC, Value: testCount00},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestProxmoxWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	got, err := (proxmoxWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: proxmoxTestUsername, secretPassword: proxmoxTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP403}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestProxmoxWidgetPollMissingURL(t *testing.T) {
	if _, err := (proxmoxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestProxmoxWidgetPollMissingSecrets(t *testing.T) {
	if _, err := (proxmoxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: "http://example.invalid"}); err == nil {
		t.Fatal("Poll() expected error for missing secrets, got nil")
	}
}

func TestProxmoxWidgetPollUnreachable(t *testing.T) {
	got, err := (proxmoxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     testUnreachableAddr,
		Secrets: map[string]string{secretUsername: proxmoxTestUsername, secretPassword: proxmoxTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestProxmoxWidgetPollInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	got, err := (proxmoxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: proxmoxTestUsername, secretPassword: proxmoxTestPassword},
		Config:  []byte(`{"insecureTLS":true}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelVMs, Value: testCount00},
		{Label: labelLXC, Value: testCount00},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestProxmoxWidgetSample(t *testing.T) {
	got := (proxmoxWidget{}).Sample(WidgetConfig{})
	if len(got) != 4 || got[0].Label != labelVMs || got[1].Label != labelLXC {
		t.Errorf("Sample() = %+v, want VMs/LXC/CPU/Memory fields", got)
	}
	assertSampleDeterministic(t, proxmoxWidget{})
}

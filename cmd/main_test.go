package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Shared literals across this file's table-driven tests, pulled out so
// goconst doesn't flag them as repeated string constants.
const (
	testSidecarContainerName = "sidecar"
	testImageRepo            = "registry.example.invalid/kubepage-operator"
	testImageRepoWithPort    = "registry.example.invalid:5000/kubepage-operator"
	testNamespaceTeamA       = "team-a"
	testNamespaceTeamB       = "team-b"
)

func TestOwnDashboardImageMissingEnvVars(t *testing.T) {
	tests := map[string]struct {
		podName, podNamespace string
	}{
		"both unset":          {podName: "", podNamespace: ""},
		"POD_NAME unset":      {podName: "", podNamespace: "ns"},
		"POD_NAMESPACE unset": {podName: "pod", podNamespace: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("POD_NAME", tc.podName)
			t.Setenv("POD_NAMESPACE", tc.podNamespace)

			_, err := ownDashboardImage(t.Context(), nil)
			if err == nil {
				t.Fatal("ownDashboardImage() error = nil, want error when POD_NAME/POD_NAMESPACE are unset")
			}
			if !strings.Contains(err.Error(), "POD_NAME and POD_NAMESPACE") {
				t.Errorf("ownDashboardImage() error = %q, want it to mention POD_NAME/POD_NAMESPACE", err.Error())
			}
		})
	}
}

// TestResolveDashboardImageOverride verifies the --dashboard-image flag path
// short-circuits before ever consulting c: passing a nil client would panic
// if resolveDashboardImage fell through to ownDashboardImage's Pod lookup, so
// a nil client here also proves the fallback path is genuinely skipped.
func TestResolveDashboardImageOverride(t *testing.T) {
	const want = "kubepage-operator:dev"
	got, err := resolveDashboardImage(t.Context(), want, nil)
	if err != nil {
		t.Fatalf("resolveDashboardImage() error = %v, want nil", err)
	}
	if got != want {
		t.Errorf("resolveDashboardImage() = %q, want %q", got, want)
	}
}

// TestResolveDashboardImageFallsBackToOwnPodLookup verifies an empty override
// falls through to ownDashboardImage's own-Pod self-lookup (the in-cluster
// path), reusing TestOwnDashboardImageMissingEnvVars' assertion that it
// surfaces the same POD_NAME/POD_NAMESPACE error when run outside a Pod.
func TestResolveDashboardImageFallsBackToOwnPodLookup(t *testing.T) {
	t.Setenv("POD_NAME", "")
	t.Setenv("POD_NAMESPACE", "")

	_, err := resolveDashboardImage(t.Context(), "", nil)
	if err == nil {
		t.Fatal("resolveDashboardImage() error = nil, want error when override is empty and POD_NAME/POD_NAMESPACE are unset")
	}
	if !strings.Contains(err.Error(), "POD_NAME and POD_NAMESPACE") {
		t.Errorf("resolveDashboardImage() error = %q, want it to mention POD_NAME/POD_NAMESPACE", err.Error())
	}
}

// TestOwnDashboardImageAgainstRealAPIServer exercises the Pod-lookup paths,
// which need a real API server to Get against (client.New(cfg, ...) talks to
// a live REST endpoint, unlike the fake client used elsewhere in this repo's
// tests) — envtest fills that role the same way internal/controller's suite
// does. No CRDs are needed since Pod is a built-in type.
func TestOwnDashboardImageAgainstRealAPIServer(t *testing.T) {
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("stopping envtest: %v", err)
		}
	})

	setupClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("building setup client: %v", err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "own-image-"}}
	if err := setupClient.Create(t.Context(), ns); err != nil {
		t.Fatalf("creating namespace: %v", err)
	}

	const wantImage = testImageRepo + ":test"

	t.Run("manager container found", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "manager-pod", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: testSidecarContainerName, Image: testSidecarContainerName + ":latest"},
					{Name: managerContainerName, Image: wantImage},
				},
			},
		}
		if err := setupClient.Create(t.Context(), pod); err != nil {
			t.Fatalf("creating pod: %v", err)
		}

		t.Setenv("POD_NAME", pod.Name)
		t.Setenv("POD_NAMESPACE", pod.Namespace)

		got, err := ownDashboardImage(t.Context(), setupClient)
		if err != nil {
			t.Fatalf("ownDashboardImage() error = %v", err)
		}
		if got != wantImage {
			t.Errorf("ownDashboardImage() = %q, want %q", got, wantImage)
		}
	})

	t.Run("manager container found with a running digest", func(t *testing.T) {
		const wantDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "manager-pod-digest", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: managerContainerName, Image: wantImage},
				},
			},
		}
		if err := setupClient.Create(t.Context(), pod); err != nil {
			t.Fatalf("creating pod: %v", err)
		}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: managerContainerName, ImageID: testImageRepo + "@" + wantDigest},
		}
		if err := setupClient.Status().Update(t.Context(), pod); err != nil {
			t.Fatalf("updating pod status: %v", err)
		}

		t.Setenv("POD_NAME", pod.Name)
		t.Setenv("POD_NAMESPACE", pod.Namespace)

		got, err := ownDashboardImage(t.Context(), setupClient)
		if err != nil {
			t.Fatalf("ownDashboardImage() error = %v", err)
		}
		want := testImageRepo + "@" + wantDigest
		if got != want {
			t.Errorf("ownDashboardImage() = %q, want %q", got, want)
		}
	})

	t.Run("manager container missing", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "no-manager-pod", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: testSidecarContainerName, Image: testSidecarContainerName + ":latest"}},
			},
		}
		if err := setupClient.Create(t.Context(), pod); err != nil {
			t.Fatalf("creating pod: %v", err)
		}

		t.Setenv("POD_NAME", pod.Name)
		t.Setenv("POD_NAMESPACE", pod.Namespace)

		_, err := ownDashboardImage(t.Context(), setupClient)
		if err == nil {
			t.Fatal("ownDashboardImage() error = nil, want error when no container is named \"manager\"")
		}
		if !strings.Contains(err.Error(), `container "manager" not found`) {
			t.Errorf("ownDashboardImage() error = %q, want it to mention the missing manager container", err.Error())
		}
	})

	t.Run("pod does not exist", func(t *testing.T) {
		t.Setenv("POD_NAME", "does-not-exist")
		t.Setenv("POD_NAMESPACE", ns.Name)

		_, err := ownDashboardImage(t.Context(), setupClient)
		if err == nil {
			t.Fatal("ownDashboardImage() error = nil, want error when the Pod doesn't exist")
		}
		if !strings.Contains(err.Error(), "getting own Pod") {
			t.Errorf("ownDashboardImage() error = %q, want it to mention getting own Pod", err.Error())
		}
	})
}

func TestParseWatchNamespaces(t *testing.T) {
	tests := map[string]struct {
		in   string
		want []string
	}{
		"empty string yields no namespaces":    {in: "", want: nil},
		"whitespace-only yields no namespaces": {in: "   ", want: nil},
		"single namespace":                     {in: testNamespaceTeamA, want: []string{testNamespaceTeamA}},
		"multiple namespaces trimmed": {
			in:   " " + testNamespaceTeamA + " , " + testNamespaceTeamB + " ,team-c",
			want: []string{testNamespaceTeamA, testNamespaceTeamB, "team-c"},
		},
		"empty entries dropped": {
			in:   testNamespaceTeamA + ",," + testNamespaceTeamB + ",",
			want: []string{testNamespaceTeamA, testNamespaceTeamB},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := parseWatchNamespaces(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("parseWatchNamespaces(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("parseWatchNamespaces(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNamespaceCacheConfigs(t *testing.T) {
	got := namespaceCacheConfigs([]string{testNamespaceTeamA, testNamespaceTeamB})
	if len(got) != 2 {
		t.Fatalf("namespaceCacheConfigs() = %v, want 2 entries", got)
	}
	if _, ok := got[testNamespaceTeamA]; !ok {
		t.Error("namespaceCacheConfigs() missing team-a")
	}
	if _, ok := got[testNamespaceTeamB]; !ok {
		t.Error("namespaceCacheConfigs() missing team-b")
	}
}

// managerStatus/sidecarStatus build a single-element ContainerStatus slice,
// pulled out so TestDigestPinnedImage's table entries stay under the
// line-length limit.
func managerStatus(imageID string) []corev1.ContainerStatus {
	return []corev1.ContainerStatus{{Name: managerContainerName, ImageID: imageID}}
}

func sidecarStatus(imageID string) []corev1.ContainerStatus {
	return []corev1.ContainerStatus{{Name: testSidecarContainerName, ImageID: imageID}}
}

func TestDigestPinnedImage(t *testing.T) {
	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const zeroDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	taggedImage := testImageRepo + ":v1.2.3"
	digestImage := testImageRepo + "@" + digest

	tests := map[string]struct {
		specImage string
		statuses  []corev1.ContainerStatus
		wantImage string
		wantOK    bool
	}{
		"tagged image gets its tag replaced by the running digest": {
			specImage: taggedImage,
			statuses:  managerStatus(digestImage),
			wantImage: digestImage,
			wantOK:    true,
		},
		"image with a registry port keeps the port, not mistaking it for a tag separator": {
			specImage: testImageRepoWithPort + ":v1.2.3",
			statuses:  managerStatus(testImageRepoWithPort + "@" + digest),
			wantImage: testImageRepoWithPort + "@" + digest,
			wantOK:    true,
		},
		"already-digest spec image is repointed at the running digest": {
			specImage: testImageRepo + "@" + zeroDigest,
			statuses:  managerStatus(digestImage),
			wantImage: digestImage,
			wantOK:    true,
		},
		"untagged image gets a digest appended": {
			specImage: testImageRepo,
			statuses:  managerStatus(digestImage),
			wantImage: digestImage,
			wantOK:    true,
		},
		"no matching container status falls back to the spec image": {
			specImage: taggedImage,
			statuses:  sidecarStatus(testSidecarContainerName + "@" + digest),
			wantOK:    false,
		},
		"empty ImageID falls back to the spec image": {
			specImage: taggedImage,
			statuses:  managerStatus(""),
			wantOK:    false,
		},
		"ImageID without a resolvable digest falls back to the spec image": {
			specImage: taggedImage,
			statuses:  managerStatus("docker://a1b2c3d4"),
			wantOK:    false,
		},
		"ImageID under a different repository (e.g. a kind-imported synthetic name) falls back to the spec image": {
			specImage: taggedImage,
			statuses:  managerStatus("docker.io/library/import-2026-07-02@" + digest),
			wantOK:    false,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotImage, gotOK := digestPinnedImage(tc.specImage, tc.statuses)
			if gotOK != tc.wantOK {
				t.Fatalf("digestPinnedImage() ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotImage != tc.wantImage {
				t.Errorf("digestPinnedImage() = %q, want %q", gotImage, tc.wantImage)
			}
		})
	}
}

func TestGatewayAPIAvailable(t *testing.T) {
	const gatewayGroup = "gateway.networking.k8s.io/v1"

	tests := map[string]struct {
		handler http.HandlerFunc
		wantOK  bool
		wantErr bool
	}{
		"HTTPRoute present": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := metav1.APIResourceList{
					GroupVersion: gatewayGroup,
					APIResources: []metav1.APIResource{
						{Kind: "Gateway"},
						{Kind: "HTTPRoute"},
						{Kind: "GRPCRoute"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantOK: true,
		},
		"group present but HTTPRoute absent": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				resp := metav1.APIResourceList{
					GroupVersion: gatewayGroup,
					APIResources: []metav1.APIResource{
						{Kind: "Gateway"},
						{Kind: "GRPCRoute"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantOK: false,
		},
		"group not found (404)": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				status := metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
					Code:   http.StatusNotFound,
				}
				_ = json.NewEncoder(w).Encode(status)
			},
			wantOK: false,
		},
		"server error": {
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			cfg := &rest.Config{Host: srv.URL}
			got, err := gatewayAPIAvailable(cfg)

			if tc.wantErr {
				if err == nil {
					t.Fatal("gatewayAPIAvailable() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("gatewayAPIAvailable() unexpected error: %v", err)
			}
			if got != tc.wantOK {
				t.Errorf("gatewayAPIAvailable() = %v, want %v", got, tc.wantOK)
			}
		})
	}
}

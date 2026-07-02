package main

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
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

	const wantImage = "registry.example.invalid/kubepage-operator:test"

	t.Run("manager container found", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "manager-pod", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sidecar", Image: "sidecar:latest"},
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
			{Name: managerContainerName, ImageID: "registry.example.invalid/kubepage-operator@" + wantDigest},
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
		want := "registry.example.invalid/kubepage-operator@" + wantDigest
		if got != want {
			t.Errorf("ownDashboardImage() = %q, want %q", got, want)
		}
	})

	t.Run("manager container missing", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "no-manager-pod", Namespace: ns.Name},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sidecar", Image: "sidecar:latest"}},
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
		"single namespace":                     {in: "team-a", want: []string{"team-a"}},
		"multiple namespaces trimmed":          {in: " team-a , team-b ,team-c", want: []string{"team-a", "team-b", "team-c"}},
		"empty entries dropped":                {in: "team-a,,team-b,", want: []string{"team-a", "team-b"}},
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
	got := namespaceCacheConfigs([]string{"team-a", "team-b"})
	if len(got) != 2 {
		t.Fatalf("namespaceCacheConfigs() = %v, want 2 entries", got)
	}
	if _, ok := got["team-a"]; !ok {
		t.Error("namespaceCacheConfigs() missing team-a")
	}
	if _, ok := got["team-b"]; !ok {
		t.Error("namespaceCacheConfigs() missing team-b")
	}
}

func TestDigestPinnedImage(t *testing.T) {
	const digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	tests := map[string]struct {
		specImage string
		statuses  []corev1.ContainerStatus
		wantImage string
		wantOK    bool
	}{
		"tagged image gets its tag replaced by the running digest": {
			specImage: "registry.example.invalid/kubepage-operator:v1.2.3",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: "registry.example.invalid/kubepage-operator@" + digest}},
			wantImage: "registry.example.invalid/kubepage-operator@" + digest,
			wantOK:    true,
		},
		"image with a registry port keeps the port, not mistaking it for a tag separator": {
			specImage: "registry.example.invalid:5000/kubepage-operator:v1.2.3",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: "registry.example.invalid:5000/kubepage-operator@" + digest}},
			wantImage: "registry.example.invalid:5000/kubepage-operator@" + digest,
			wantOK:    true,
		},
		"already-digest spec image is repointed at the running digest": {
			specImage: "registry.example.invalid/kubepage-operator@sha256:0000000000000000000000000000000000000000000000000000000000000000",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: "registry.example.invalid/kubepage-operator@" + digest}},
			wantImage: "registry.example.invalid/kubepage-operator@" + digest,
			wantOK:    true,
		},
		"untagged image gets a digest appended": {
			specImage: "registry.example.invalid/kubepage-operator",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: "registry.example.invalid/kubepage-operator@" + digest}},
			wantImage: "registry.example.invalid/kubepage-operator@" + digest,
			wantOK:    true,
		},
		"no matching container status falls back to the spec image": {
			specImage: "registry.example.invalid/kubepage-operator:v1.2.3",
			statuses:  []corev1.ContainerStatus{{Name: "sidecar", ImageID: "sidecar@" + digest}},
			wantOK:    false,
		},
		"empty ImageID falls back to the spec image": {
			specImage: "registry.example.invalid/kubepage-operator:v1.2.3",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: ""}},
			wantOK:    false,
		},
		"ImageID without a resolvable digest falls back to the spec image": {
			specImage: "registry.example.invalid/kubepage-operator:v1.2.3",
			statuses:  []corev1.ContainerStatus{{Name: managerContainerName, ImageID: "docker://a1b2c3d4"}},
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

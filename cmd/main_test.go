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

			_, err := ownDashboardImage(t.Context(), nil, scheme)
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

		got, err := ownDashboardImage(t.Context(), cfg, scheme)
		if err != nil {
			t.Fatalf("ownDashboardImage() error = %v", err)
		}
		if got != wantImage {
			t.Errorf("ownDashboardImage() = %q, want %q", got, wantImage)
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

		_, err := ownDashboardImage(t.Context(), cfg, scheme)
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

		_, err := ownDashboardImage(t.Context(), cfg, scheme)
		if err == nil {
			t.Fatal("ownDashboardImage() error = nil, want error when the Pod doesn't exist")
		}
		if !strings.Contains(err.Error(), "getting own Pod") {
			t.Errorf("ownDashboardImage() error = %q, want it to mention getting own Pod", err.Error())
		}
	})
}

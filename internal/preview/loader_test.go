package preview

import (
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := pagev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const dashboardYAML = `apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: sample
spec: {}
`

func TestCollectFilesSingleFile(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	files, err := collectFiles([]string{f}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != f {
		t.Fatalf("files = %v, want [%s]", files, f)
	}
}

func TestCollectFilesDirectoryNonRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", dashboardYAML)
	writeFile(t, dir, "b.yml", dashboardYAML)
	writeFile(t, dir, "readme.md", "not yaml")

	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sub, "c.yaml", dashboardYAML)

	files, err := collectFiles([]string{dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("non-recursive walk got %v, want 2 files (a.yaml, b.yml)", files)
	}
}

func TestCollectFilesDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", dashboardYAML)
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sub, "b.yaml", dashboardYAML)

	files, err := collectFiles([]string{dir}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("recursive walk got %v, want 2 files", files)
	}
}

func TestDecodeFilesMultiDocument(t *testing.T) {
	dir := t.TempDir()
	multi := dashboardYAML + "---\n" + `apiVersion: page.kubepage.dev/v1alpha1
kind: Bookmark
metadata:
  name: gh
spec:
  dashboardRef:
    name: sample
  group: Dev
  name: Github
  href: https://github.com/
`
	f := writeFile(t, dir, "multi.yaml", multi)

	objs, err := decodeFiles(testScheme(t), []string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2", len(objs))
	}
	if _, ok := objs[0].(*pagev1alpha1.Dashboard); !ok {
		t.Errorf("objs[0] = %T, want *Dashboard", objs[0])
	}
	if _, ok := objs[1].(*pagev1alpha1.Bookmark); !ok {
		t.Errorf("objs[1] = %T, want *Bookmark", objs[1])
	}
}

func TestDecodeFilesSkipsUnrecognizedKind(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "crd.yaml", `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec: {}
`)

	// testScheme only registers corev1 + pagev1alpha1, so
	// CustomResourceDefinition is an unrecognized (not just undecoded) kind.
	objs, err := decodeFiles(testScheme(t), []string{f})
	if err != nil {
		t.Fatalf("expected unrecognized kinds to be skipped, not error: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("got %d objects, want 0", len(objs))
	}
}

func TestDecodeFilesSkipsNonKubernetesDocument(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "kustomization.yaml", `resources:
- dashboard.yaml
`)

	objs, err := decodeFiles(testScheme(t), []string{f})
	if err != nil {
		t.Fatalf("expected a document with no kind to be skipped, not error: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("got %d objects, want 0", len(objs))
	}
}

func TestDecodeFilesResolvesSecret(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "secret.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: plex-credentials
stringData:
  token: hunter2
data:
  existing: dW50b3VjaGVk
`)

	objs, err := decodeFiles(testScheme(t), []string{f})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("got %d objects, want 1", len(objs))
	}
	secret, ok := objs[0].(*corev1.Secret)
	if !ok {
		t.Fatalf("objs[0] = %T, want *Secret", objs[0])
	}
	// stringData is normalized into Data (mirroring the apiserver's own
	// create strategy, which the fake client never runs) since every
	// consumer (poller.go's resolveSecret, auth.go's loadBasicAuth) reads
	// only Data — see normalizeSecretStringData.
	if string(secret.Data["token"]) != "hunter2" {
		t.Errorf("Data[token] = %q, want hunter2", secret.Data["token"])
	}
	if string(secret.Data["existing"]) != "untouched" {
		t.Errorf("Data[existing] = %q, want untouched (base64-decoded, untouched)", secret.Data["existing"])
	}
	if secret.StringData != nil {
		t.Errorf("StringData = %v, want nil after normalization", secret.StringData)
	}
}

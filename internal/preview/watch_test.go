package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestSwappableReaderDelegatesToCurrent(t *testing.T) {
	scheme := testScheme(t)
	first := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "a"}},
	).Build()
	second := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&pagev1alpha1.Bookmark{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "b"}},
	).Build()

	sw := NewSwappableReader(first)

	var bm pagev1alpha1.Bookmark
	if err := sw.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "a"}, &bm); err != nil {
		t.Fatalf("Get(a) before swap: %v", err)
	}

	sw.Store(second)

	if err := sw.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "a"}, &bm); err == nil {
		t.Error("Get(a) after swap should fail: 'a' only exists in the pre-swap reader")
	}
	if err := sw.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "b"}, &bm); err != nil {
		t.Errorf("Get(b) after swap: %v", err)
	}

	var list pagev1alpha1.BookmarkList
	if err := sw.List(context.Background(), &list); err != nil {
		t.Fatalf("List after swap: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "b" {
		t.Errorf("List after swap = %v, want just [b]", list.Items)
	}
}

func TestWatchDirsFile(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	dirs, err := watchDirs([]string{f}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0] != dir {
		t.Errorf("watchDirs(file) = %v, want [%s] (the file's parent)", dirs, dir)
	}
}

func TestWatchDirsDirectoryNonRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	dirs, err := watchDirs([]string{dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0] != dir {
		t.Errorf("watchDirs(dir, non-recursive) = %v, want just [%s]", dirs, dir)
	}
}

func TestWatchDirsDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}

	dirs, err := watchDirs([]string{dir}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 {
		t.Errorf("watchDirs(dir, recursive) = %v, want [%s, %s]", dirs, dir, sub)
	}
}

// TestWatchReloadsOnFileChange is an end-to-end test of Watch: it writes an
// initial Dashboard-only directory, starts Watch, then adds a Bookmark file
// and asserts the SwappableReader picks it up without any explicit reload
// call — the same mechanism cmd/main.go's preview subcommand relies on for
// "save the YAML, see it update".
func TestWatchReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	scheme := testScheme(t)
	result, err := Load(Config{Scheme: scheme, Paths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	reader := NewSwappableReader(result.Reader)

	ctx := t.Context()

	cfg := Config{Scheme: scheme, Paths: []string{dir}}
	if err := Watch(ctx, cfg, result.Namespace, result.DashboardName, reader); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Not present yet.
	var list pagev1alpha1.BookmarkList
	if err := reader.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("bookmarks before write = %v, want none", list.Items)
	}

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

	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := reader.List(context.Background(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Watch did not pick up the new Bookmark within 5s (items=%v)", list.Items)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestWatchKeepsLastGoodConfigOnBrokenReload writes a file that fails to
// parse after Watch has started, and asserts the SwappableReader keeps
// serving the last-good config rather than erroring or going empty.
func TestWatchKeepsLastGoodConfigOnBrokenReload(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	scheme := testScheme(t)
	result, err := Load(Config{Scheme: scheme, Paths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	reader := NewSwappableReader(result.Reader)

	ctx := t.Context()

	cfg := Config{Scheme: scheme, Paths: []string{dir}}
	if err := Watch(ctx, cfg, result.Namespace, result.DashboardName, reader); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Break the only Dashboard's YAML syntax.
	writeFile(t, dir, "dashboard.yaml", "not: [valid yaml")
	time.Sleep(2 * debounceInterval)

	var dash pagev1alpha1.Dashboard
	if err := reader.Get(context.Background(),
		types.NamespacedName{Namespace: result.Namespace, Name: result.DashboardName}, &dash); err != nil {
		t.Errorf("Get(sample Dashboard) after a broken reload: %v, want the last-good config to still be served", err)
	}
}

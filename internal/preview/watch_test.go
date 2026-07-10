package preview

import (
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
	if err := sw.Get(t.Context(), types.NamespacedName{Namespace: "ns", Name: "a"}, &bm); err != nil {
		t.Fatalf("Get(a) before swap: %v", err)
	}

	sw.Store(second)

	if err := sw.Get(t.Context(), types.NamespacedName{Namespace: "ns", Name: "a"}, &bm); err == nil {
		t.Error("Get(a) after swap should fail: 'a' only exists in the pre-swap reader")
	}
	if err := sw.Get(t.Context(), types.NamespacedName{Namespace: "ns", Name: "b"}, &bm); err != nil {
		t.Errorf("Get(b) after swap: %v", err)
	}

	var list pagev1alpha1.BookmarkList
	if err := sw.List(t.Context(), &list); err != nil {
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
	if err := reader.List(t.Context(), &list); err != nil {
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
		if err := reader.List(t.Context(), &list); err != nil {
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

// TestWatchNamespacePinSurvivesDashboardGainingExplicitNamespace verifies
// the fix for reload pinning to the caller's *original* namespace filter
// (often empty) rather than Load's resolved/defaulted namespace: pinning to
// the resolved value would make a later edit that gives the Dashboard its
// own explicit metadata.namespace fail to match on reload, since
// selectDashboard would compare that new namespace against the earlier
// default instead of against what the user actually asked for (see Watch's
// doc comment). cmd/main.go passes Config.Namespace, not Result.Namespace,
// for exactly this reason — this test calls Watch the same way.
func TestWatchNamespacePinSurvivesDashboardGainingExplicitNamespace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dashboard.yaml", dashboardYAML) // no namespace set

	scheme := testScheme(t)
	cfg := Config{Scheme: scheme, Paths: []string{dir}} // cfg.Namespace == ""
	result, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Namespace != defaultNamespace {
		t.Fatalf("Namespace = %q, want the defaulted %q", result.Namespace, defaultNamespace)
	}
	reader := NewSwappableReader(result.Reader)

	if err := Watch(t.Context(), cfg, cfg.Namespace, result.DashboardName, reader); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Give the Dashboard an explicit namespace different from the earlier
	// default ("preview") — with the bug, this reload would find no
	// candidate (its namespace no longer matches the pinned "preview") and
	// silently keep serving the pre-edit config.
	writeFile(t, dir, "dashboard.yaml", `apiVersion: page.kubepage.dev/v1alpha1
kind: Dashboard
metadata:
  name: sample
  namespace: custom-ns
spec: {}
`)

	deadline := time.Now().Add(5 * time.Second)
	for {
		var dash pagev1alpha1.Dashboard
		err := reader.Get(t.Context(), types.NamespacedName{Namespace: "custom-ns", Name: "sample"}, &dash)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reload never picked up the explicitly-namespaced Dashboard: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestWatchIgnoresChmodOnlyEvents verifies a metadata-only change (chmod,
// with no content write) doesn't trigger a full reload — checked by
// comparing the SwappableReader's underlying pointer identity, since Load
// always builds a brand new fake client.
func TestWatchIgnoresChmodOnlyEvents(t *testing.T) {
	dir := t.TempDir()
	f := writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	scheme := testScheme(t)
	cfg := Config{Scheme: scheme, Paths: []string{dir}}
	result, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewSwappableReader(result.Reader)

	if err := Watch(t.Context(), cfg, cfg.Namespace, result.DashboardName, reader); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	before := reader.current.Load()

	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * debounceInterval)

	after := reader.current.Load()
	if before != after {
		t.Error("a chmod-only change triggered a reload (Reader was swapped), want it ignored")
	}
}

// TestWatchAddsNewSubdirectoriesWhenRecursive verifies a directory created
// after Watch has started is itself watched (when Recursive is set), so a
// file later saved inside it triggers a reload — without this, --recursive
// would only ever cover the directory tree that existed at startup.
func TestWatchAddsNewSubdirectoriesWhenRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dashboard.yaml", dashboardYAML)

	scheme := testScheme(t)
	cfg := Config{Scheme: scheme, Paths: []string{dir}, Recursive: true}
	result, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reader := NewSwappableReader(result.Reader)

	if err := Watch(t.Context(), cfg, cfg.Namespace, result.DashboardName, reader); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	sub := filepath.Join(dir, "newteam")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sub, "bookmark.yaml", `apiVersion: page.kubepage.dev/v1alpha1
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
		var list pagev1alpha1.BookmarkList
		if err := reader.List(t.Context(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Watch never picked up a file saved in a post-startup subdirectory (items=%v)", list.Items)
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
	if err := reader.Get(t.Context(),
		types.NamespacedName{Namespace: result.Namespace, Name: result.DashboardName}, &dash); err != nil {
		t.Errorf("Get(sample Dashboard) after a broken reload: %v, want the last-good config to still be served", err)
	}
}

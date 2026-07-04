package preview

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/maverickd650/kubepage-operator/internal/dashboard"
)

// debounceInterval coalesces a burst of filesystem events (e.g. an editor's
// write-via-rename, which fires both a Remove and a Create for one save)
// into a single reload.
const debounceInterval = 200 * time.Millisecond

// SwappableReader is a client.Reader whose backing Reader can be replaced
// atomically, so Watch can hot-swap in newly reloaded manifests without
// Server/Poller ever needing to know their Reader changed underneath them —
// they only ever hold a *SwappableReader, not the concrete Reader it wraps.
type SwappableReader struct {
	current atomic.Pointer[client.Reader]
}

// NewSwappableReader returns a SwappableReader initially backed by initial.
func NewSwappableReader(initial client.Reader) *SwappableReader {
	s := &SwappableReader{}
	s.Store(initial)
	return s
}

// Store atomically replaces the Reader every subsequent Get/List is served
// from.
func (s *SwappableReader) Store(r client.Reader) {
	s.current.Store(&r)
}

func (s *SwappableReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return (*s.current.Load()).Get(ctx, key, obj, opts...)
}

func (s *SwappableReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return (*s.current.Load()).List(ctx, list, opts...)
}

// Watch starts a background fsnotify watcher over every directory reachable
// from cfg.Paths and, on a .yaml/.yml change, reloads the manifests and
// swaps the result into reader. namespace/dashboardName pin the reload's
// selectDashboard call (cfg's own Namespace/DashboardName are ignored):
// dashboardName should be the already-resolved target Dashboard's name
// (Load's Result.DashboardName), but namespace should be the caller's
// original, pre-resolution namespace filter (Config.Namespace as the user
// supplied it — often empty) rather than Result.Namespace. Pinning to the
// *resolved* (possibly defaulted) namespace instead would make a later edit
// that gives the Dashboard its own explicit metadata.namespace fail to
// match on reload, since selectDashboard would then compare that namespace
// against the earlier default rather than against what the user actually
// asked for. An empty namespace filter still can't turn ambiguous, because
// dashboardName alone is enough to identify one Dashboard as long as no
// second Dashboard is later added under the same name.
//
// A reload that fails to parse or match is logged and reader keeps serving
// its last-good value: a syntax error (or a still-genuinely-ambiguous
// Dashboard set) mid-edit shouldn't take down a running preview.
//
// Watch returns once the watcher is established; it keeps running in the
// background until ctx is done.
func Watch(ctx context.Context, cfg Config, namespace, dashboardName string, reader *SwappableReader) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("starting file watcher: %w", err)
	}

	dirs, err := watchDirs(cfg.Paths, cfg.Recursive)
	if err != nil {
		_ = watcher.Close()
		return err
	}
	for _, d := range dirs {
		if err := watcher.Add(d); err != nil {
			_ = watcher.Close()
			return fmt.Errorf("watching %s: %w", d, err)
		}
	}

	reloadCfg := cfg
	reloadCfg.Namespace = namespace
	reloadCfg.DashboardName = dashboardName

	go runWatchLoop(ctx, watcher, reloadCfg, reader)
	return nil
}

func runWatchLoop(ctx context.Context, watcher *fsnotify.Watcher, cfg Config, reader *SwappableReader) {
	defer func() { _ = watcher.Close() }()

	reload := func() {
		result, err := Load(cfg)
		if err != nil {
			log.Info("Reload failed, keeping previous config", "error", err.Error())
			return
		}
		reader.Store(result.Reader)
		// Swapping the Reader doesn't touch dashboard's own package-level
		// basic-auth cache, so a changed htpasswd Secret would otherwise
		// keep enforcing pre-edit credentials for up to its TTL.
		dashboard.InvalidateAuthCache(result.Namespace, result.DashboardName)
		log.Info("Reloaded preview manifests")
	}

	var timer *time.Timer
	scheduleReload := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounceInterval, reload)
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// A directory created under a recursively-watched tree needs
			// its own explicit watcher.Add: fsnotify only watches the
			// directories it was told about when Watch started, never
			// their future descendants — see watchDirs' doc comment.
			// Without this, files saved under a subdirectory created after
			// startup would never fire an event on this watcher at all.
			if cfg.Recursive && event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					if err := watcher.Add(event.Name); err != nil {
						log.Info("Failed to watch new directory", "dir", event.Name, "error", err.Error())
					}
					scheduleReload() // pick up any files already inside it
					continue
				}
			}

			if !isYAMLFile(event.Name) {
				continue
			}
			// A pure Chmod (a permission or mtime-only touch, with no
			// content change — some editors/tools emit these on save
			// alongside a real Write) isn't worth a full re-parse and
			// fake-client rebuild.
			if !event.Has(fsnotify.Write | fsnotify.Create | fsnotify.Remove | fsnotify.Rename) {
				continue
			}
			scheduleReload()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Info("Watcher error", "error", err.Error())
		}
	}
}

// watchDirs returns the set of directories Watch should register with
// fsnotify to observe every file collectFiles(paths, recursive) would find:
// a plain file's own parent directory (fsnotify watches directories, and an
// editor's atomic save replaces the file via rename, which requires
// watching the directory rather than the file's original inode), a
// directory itself, and — when recursive — every subdirectory beneath it.
func watchDirs(paths []string, recursive bool) ([]string, error) {
	seen := make(map[string]bool)
	var dirs []string
	add := func(d string) {
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
		if !info.IsDir() {
			add(filepath.Dir(p))
			continue
		}

		add(p)
		if !recursive {
			continue
		}
		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				add(path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", p, err)
		}
	}
	return dirs, nil
}

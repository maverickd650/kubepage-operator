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
// swaps the result into reader. namespace/dashboardName pin the reload to
// the already-resolved target Dashboard (cfg's own Namespace/DashboardName
// are ignored) so a later edit — e.g. adding a second Dashboard file — can't
// turn a hot reload ambiguous partway through a session. A reload that
// fails to parse is logged and reader keeps serving its last-good value: a
// syntax error mid-edit shouldn't take down a running preview.
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
		log.Info("Reloaded preview manifests")
	}

	var timer *time.Timer
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
			if !isYAMLFile(event.Name) {
				continue
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounceInterval, reload)
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

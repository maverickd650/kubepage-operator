// Package preview loads Dashboard/DashboardStyle/ServiceCard/Bookmark/
// InfoWidget/Secret manifests from local YAML files into an in-memory
// client.Client, so internal/dashboard's Server/Poller can render a
// Dashboard without a real cluster. See cmd/main.go's "preview" subcommand
// and docs/design/local-preview.md for the full design.
package preview

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("preview")

// collectFiles resolves paths (files or directories) to a sorted list of
// .yaml/.yml files, mirroring `kubectl apply -f`'s own semantics: a
// directory is walked one level deep unless recursive is set. Sorting makes
// load order (and thus any "last one wins" behavior, though none is
// expected here) deterministic across platforms/filesystems.
func collectFiles(paths []string, recursive bool) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}

		err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if !recursive && path != p {
					return filepath.SkipDir
				}
				return nil
			}
			if isYAMLFile(path) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walking %s: %w", p, err)
		}
	}
	slices.Sort(files)
	return files, nil
}

func isYAMLFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// decodeFiles parses every YAML document in files through scheme's
// deserializer. A document whose apiVersion/kind isn't registered in scheme
// (e.g. a Deployment, or some other kind not relevant to the dashboard) is
// skipped with a logged warning rather than failing the whole load, so
// pointing -f at a whole GitOps directory or kustomize output just works.
func decodeFiles(scheme *runtime.Scheme, files []string) ([]client.Object, error) {
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	var objs []client.Object
	for _, f := range files {
		data, err := os.ReadFile(f) // nolint:gosec // f comes from collectFiles walking user-supplied -f paths, not untrusted input
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		yamlReader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for {
			doc, err := yamlReader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", f, err)
			}
			doc = bytes.TrimSpace(doc)
			if len(doc) == 0 {
				continue
			}

			obj, gvk, err := decoder.Decode(doc, nil, nil)
			if err != nil {
				switch {
				case runtime.IsNotRegisteredError(err):
					log.Info("Skipped unrecognized kind", "file", f)
					continue
				case runtime.IsMissingKind(err):
					// Not a Kubernetes object at all (e.g. kustomization.yaml,
					// a Helm values.yaml) — expected when -f points at a whole
					// directory rather than a curated set of CR manifests.
					log.Info("Skipped non-Kubernetes document", "file", f)
					continue
				}
				return nil, fmt.Errorf("decoding %s: %w", f, err)
			}

			co, ok := obj.(client.Object)
			if !ok {
				log.Info("Skipped non-object kind", "file", f, "kind", gvk.Kind)
				continue
			}
			if secret, ok := co.(*corev1.Secret); ok {
				normalizeSecretStringData(secret)
			}
			objs = append(objs, co)
		}
	}
	return objs, nil
}

// normalizeSecretStringData copies secret.StringData into secret.Data,
// mirroring the apiserver's own create/update strategy — a step that never
// runs here, since preview only decodes client-side and stores straight into
// a fake client. Without this, a Secret authored the idiomatic way
// (stringData:, not base64 data:) would decode with an empty Data map, and
// every consumer (poller.go's resolveSecret, auth.go's loadBasicAuth) reads
// only .Data, so widgets/basic-auth referencing it would fail as if the key
// didn't exist. StringData is cleared afterward to match the apiserver's own
// behavior, where it's a write-only convenience field never persisted back.
func normalizeSecretStringData(secret *corev1.Secret) {
	if len(secret.StringData) == 0 {
		return
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	for k, v := range secret.StringData {
		secret.Data[k] = []byte(v)
	}
	secret.StringData = nil
}

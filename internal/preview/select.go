package preview

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// defaultNamespace is used when the selected Dashboard itself carries no
// metadata.namespace (common for hand-written sample YAML, e.g.
// config/samples/page_v1alpha1_dashboard.yaml).
const defaultNamespace = "preview"

// Config is preview.Load's input: where to read manifests from and which
// Dashboard among them to serve.
type Config struct {
	// Scheme must have corev1 (for Secret) and pagev1alpha1 registered; the
	// caller (cmd/main.go) already builds one scheme shared by every mode.
	Scheme *runtime.Scheme

	// Paths are files and/or directories to load; at least one is required.
	Paths []string
	// Recursive walks directories in Paths recursively; otherwise only their
	// direct children are considered, matching `kubectl apply -f`.
	Recursive bool

	// Namespace and DashboardName select which Dashboard among the loaded
	// objects to serve. Both are optional as long as exactly one Dashboard
	// is present in Paths.
	Namespace     string
	DashboardName string
}

// Result is what cmd/main.go needs to start dashboard.RunPreview.
type Result struct {
	// Reader serves every loaded object (Get/List), standing in for the
	// cluster-cache and secret clients dashboard.Run builds from a
	// rest.Config.
	Reader client.Reader

	// Namespace and DashboardName are the resolved target, after defaulting
	// — always non-empty.
	Namespace     string
	DashboardName string
}

// Load parses cfg.Paths into typed objects, selects the target Dashboard,
// defaults every object's empty namespace to match it, and returns an
// in-memory Reader over the result.
func Load(cfg Config) (*Result, error) {
	files, err := collectFiles(cfg.Paths, cfg.Recursive)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .yaml/.yml files found in %s", strings.Join(cfg.Paths, ", "))
	}

	objs, err := decodeFiles(cfg.Scheme, files)
	if err != nil {
		return nil, err
	}

	dash, err := selectDashboard(objs, cfg.Namespace, cfg.DashboardName)
	if err != nil {
		return nil, err
	}

	applyDefaultNamespace(objs, dash)

	return &Result{
		Reader:        fake.NewClientBuilder().WithScheme(cfg.Scheme).WithObjects(objs...).Build(),
		Namespace:     dash.Namespace,
		DashboardName: dash.Name,
	}, nil
}

// selectDashboard picks the one Dashboard cmd/main.go's preview subcommand
// should serve. With exactly one Dashboard among objs and no namespace/name
// override, that Dashboard is returned; a Dashboard whose own namespace is
// still empty is treated as compatible with any --namespace, since
// applyDefaultNamespace resolves it afterward. Zero or multiple matches is
// an error listing the candidates, so the caller can pass --namespace/
// --dashboard-name to disambiguate rather than silently guessing.
func selectDashboard(objs []client.Object, namespace, dashboardName string) (*pagev1alpha1.Dashboard, error) {
	var all, candidates []*pagev1alpha1.Dashboard
	for _, o := range objs {
		d, ok := o.(*pagev1alpha1.Dashboard)
		if !ok {
			continue
		}
		all = append(all, d)
		if dashboardName != "" && d.Name != dashboardName {
			continue
		}
		if namespace != "" && d.Namespace != "" && d.Namespace != namespace {
			continue
		}
		candidates = append(candidates, d)
	}

	switch len(candidates) {
	case 0:
		if len(all) == 0 {
			return nil, fmt.Errorf("no Dashboard object found in the given -f paths")
		}
		return nil, fmt.Errorf("no Dashboard matches --namespace=%q --dashboard-name=%q; found: %s",
			namespace, dashboardName, describeDashboards(all))
	case 1:
		return candidates[0], nil
	default:
		hint := "pass --namespace/--dashboard-name to select one"
		if !anyNamespaced(candidates) {
			// --namespace can't disambiguate these: selectDashboard treats
			// an empty metadata.namespace as compatible with any --namespace
			// value (see this function's own doc comment), so every one of
			// these candidates would still match regardless of what's
			// passed. Only --dashboard-name actually narrows the set.
			hint = "pass --dashboard-name to select one (none of these set metadata.namespace, so --namespace can't tell them apart)"
		}
		return nil, fmt.Errorf("multiple Dashboards found, %s: %s", hint, describeDashboards(candidates))
	}
}

// anyNamespaced reports whether at least one Dashboard in ds sets its own
// metadata.namespace, for selectDashboard's ambiguous-match error message.
func anyNamespaced(ds []*pagev1alpha1.Dashboard) bool {
	for _, d := range ds {
		if d.Namespace != "" {
			return true
		}
	}
	return false
}

func describeDashboards(ds []*pagev1alpha1.Dashboard) string {
	names := make([]string, len(ds))
	for i, d := range ds {
		ns := d.Namespace
		if ns == "" {
			ns = "<none>"
		}
		names[i] = ns + "/" + d.Name
	}
	return strings.Join(names, ", ")
}

// applyDefaultNamespace fills in every object's empty metadata.namespace
// (including dash's own, if unset) with dash's namespace, falling back to
// defaultNamespace when dash has none either — local YAML (samples, a
// user's own drafts) frequently omits namespace entirely.
func applyDefaultNamespace(objs []client.Object, dash *pagev1alpha1.Dashboard) {
	ns := dash.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	for _, o := range objs {
		if o.GetNamespace() == "" {
			o.SetNamespace(ns)
		}
	}
}

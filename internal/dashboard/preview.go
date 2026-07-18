package dashboard

import (
	"context"
	"errors"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PreviewOptions configures RunPreview: everything Options needs except
// RestConfig/Scheme/GatewayAPIEnabled, which have no meaning without a real
// cluster. Reader stands in for the cluster-cache and secret clients Run
// builds from a rest.Config — see internal/preview for how it's constructed
// from local YAML manifests.
type PreviewOptions struct {
	Reader client.Reader

	Namespace     string
	DashboardName string

	Addr         string
	MetricsAddr  string
	PollInterval time.Duration

	// Version/Commit are stamped at build time (cmd/main.go's ldflags-set
	// package vars), shown in the page shell's footer unless the bound
	// spec.style sets HideVersion.
	Version string
	Commit  string

	// Ready, if set, is called once the main HTTP listener is bound, with
	// the actual resolved address — see Options.Ready's doc comment.
	Ready func(addr string)

	// SampleData enables --sample-data mode: every widget/monitor probe is
	// replaced by canned placeholder data (see Poller.SampleData), so a
	// preview renders fully populated cards without a reachable upstream or
	// local copies of any secret material the loaded YAML's secretKeyRefs
	// point at. Independent of internalUrl handling: RunPreview always
	// ignores internalUrl (see Poller.Preview), whether or not SampleData is
	// set.
	SampleData bool
}

// errNoCluster is returned by noopClusterReader for every Get/List, so
// ClusterWidget types (kubemetrics) see it the same way they'd see any other
// unreachable upstream and render their normal error state — see
// kubemetrics.go's PollCluster, which treats a List error as "unreachable"
// rather than propagating it.
var errNoCluster = errors.New("preview: no cluster available for cluster-scoped widgets")

// noopClusterReader stands in for RunPreview's KubeReader: preview mode has
// no real cluster to source ClusterWidget data (nodes, metrics.k8s.io) from.
type noopClusterReader struct{}

func (noopClusterReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errNoCluster
}

func (noopClusterReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errNoCluster
}

// RunPreview serves the dashboard against opts.Reader instead of a live
// cluster, for local development without installing the operator (see
// cmd/main.go's "preview" subcommand and internal/preview). Widget polling
// still makes real outbound HTTP requests to whatever URLs the loaded
// ServiceCards name — only Kubernetes API access (CRD reads, Secret
// resolution) is backed by opts.Reader rather than a cluster — unless
// opts.SampleData is set, in which case no widget/monitor ever makes a real
// network call or reads a Secret at all (see Poller.SampleData); a
// kubemetrics InfoWidget then shows its Sample output instead of erroring
// through noopClusterReader below. GatewayAPIEnabled is always false:
// HTTPRoute discovery has no meaning without a cluster. Options.Preview is
// always set true here (never user-configurable, unlike SampleData): a
// laptop can never reach a cluster-internal URL, so internalUrl is always
// ignored regardless of --sample-data — see Poller.Preview's doc comment for
// what that gap is covered by.
func RunPreview(ctx context.Context, opts PreviewOptions) error {
	return serve(ctx, Options{
		Namespace:         opts.Namespace,
		DashboardName:     opts.DashboardName,
		Addr:              opts.Addr,
		MetricsAddr:       opts.MetricsAddr,
		PollInterval:      opts.PollInterval,
		Version:           opts.Version,
		Commit:            opts.Commit,
		GatewayAPIEnabled: false,
		Ready:             opts.Ready,
		SampleData:        opts.SampleData,
		Preview:           true,
	}, opts.Reader, opts.Reader, noopClusterReader{})
}

package dashboard

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// schemeHTTP and schemeHTTPS are the two URL schemes this package derives
// from or switches on in more than one place (ingressHref below, truenas.go's
// http(s)->ws(s) scheme mapping), pulled out so goconst doesn't flag the
// repeated literal.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// defaultDiscoveryPrefix is DiscoverySpec.AnnotationPrefix's default.
const defaultDiscoveryPrefix = "kubepage.io/"

// homepageDiscoveryPrefix is the annotation prefix DiscoverySpec.HomepageCompat
// additionally honors, matching homepage's own Ingress discovery convention
// (https://gethomepage.dev/configs/kubernetes/) so a cluster migrating from
// homepage doesn't need to relabel every Ingress.
const homepageDiscoveryPrefix = "gethomepage.dev/"

// Annotation keys, relative to whichever prefix matched (see
// discoveryAnnotations below).
const (
	discoveryAnnEnabled     = "enabled"
	discoveryAnnName        = "name"
	discoveryAnnGroup       = "group"
	discoveryAnnIcon        = "icon"
	discoveryAnnHref        = "href"
	discoveryAnnDescription = "description"
	discoveryAnnMonitor     = "monitor"

	// discoveryAnnPing is homepage's own name for the monitor flag
	// ("gethomepage.dev/ping"), still honored — alongside "monitor" — so
	// HomepageCompat keeps working with unmodified homepage annotations.
	discoveryAnnPing = "ping"
)

// defaultDiscoveryGroup is the Group a discovered card renders under when
// its Ingress sets no group annotation.
const defaultDiscoveryGroup = "Discovered"

// annotationValueTrue is the boolean-flag value Ingress annotations use for
// "enabled"/"monitor" (e.g. "kubepage.io/enabled: \"true\""), matching
// homepage's own annotation convention.
const annotationValueTrue = "true"

// discoveredService is a service card synthesized from an annotated Ingress,
// good for exactly what an Ingress annotation can safely carry: title/icon/
// description/href/monitor. Never a polled widget — see DiscoverySpec's doc
// comment on why annotations can't carry secrets.
type discoveredService struct {
	Key         string
	Group       string
	Name        string
	IconURL     string
	Description string
	Href        string
	Monitor     bool
}

// extraDiscoveryNamespaces returns spec.Namespaces with namespace (the
// Dashboard's own) filtered out: it's already covered by reader/namespace in
// discoverServices/discoverHTTPRoutes, so passing it through to extraReader
// too would just list it twice (harmless — Store dedupes by
// discoveredService.Key — but pointless work and an extra List call).
func extraDiscoveryNamespaces(spec pagev1alpha1.DiscoverySpec, namespace string) []string {
	out := make([]string, 0, len(spec.Namespaces))
	for _, ns := range spec.Namespaces {
		if ns != namespace {
			out = append(out, ns)
		}
	}
	return out
}

// discoverServices lists every Ingress in namespace, plus (via extraReader)
// every Ingress in each of extraNamespaces — see DiscoverySpec.Namespaces'
// doc comment — and returns the ones that opt into discovery via annotation,
// per spec's AnnotationPrefix/HomepageCompat. extraReader is expected to be
// an uncached client: unlike reader (the Dashboard's own namespace-scoped
// informer cache), extraNamespaces is an arbitrary, spec-driven set that a
// single namespace-scoped cache can't cover, and the cross-namespace RBAC
// backing it (internal/controller's reconcileDiscoveryRBAC) is itself
// reconciled on the same cadence as any other spec change, so there's no
// long-lived cache to keep in sync.
func discoverServices(ctx context.Context, reader client.Reader, namespace string, extraReader client.Reader, extraNamespaces []string, spec pagev1alpha1.DiscoverySpec) ([]discoveredService, error) {
	var ingresses networkingv1.IngressList
	if err := reader.List(ctx, &ingresses, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Ingresses: %w", err)
	}
	// A failure listing one extra namespace (e.g. a namespace named in
	// spec.discovery.namespaces that doesn't exist, or whose RoleBinding
	// hasn't been (re)created yet) degrades that namespace only, rather than
	// failing the whole discovery pass — the caller (Poller.pollOnce) would
	// otherwise prune every previously-discovered card, including
	// perfectly-healthy own-namespace ones, on every single poll cycle
	// until the one bad namespace is fixed.
	for _, ns := range extraNamespaces {
		var extra networkingv1.IngressList
		if err := extraReader.List(ctx, &extra, client.InNamespace(ns)); err != nil {
			pollerLog.Error(err, "discovering Ingresses in extra namespace", "namespace", ns)
			continue
		}
		ingresses.Items = append(ingresses.Items, extra.Items...)
	}

	prefix := defaultDiscoveryPrefix
	if spec.AnnotationPrefix != nil && *spec.AnnotationPrefix != "" {
		prefix = *spec.AnnotationPrefix
	}
	homepageCompat := spec.HomepageCompat != nil && *spec.HomepageCompat

	out := make([]discoveredService, 0, len(ingresses.Items))
	for _, ing := range ingresses.Items {
		ann, ok := discoveryAnnotations(ing.Annotations, prefix, homepageCompat)
		if !ok {
			continue
		}

		name := cmp.Or(ann[discoveryAnnName], ing.Name)
		group := cmp.Or(ann[discoveryAnnGroup], defaultDiscoveryGroup)
		href := cmp.Or(ann[discoveryAnnHref], ingressHref(&ing))

		var iconURL string
		if icon := ann[discoveryAnnIcon]; icon != "" {
			iconURL = IconURL(&icon)
		}

		out = append(out, discoveredService{
			Key:         "discovery/" + ing.Namespace + "/" + ing.Name,
			Group:       group,
			Name:        name,
			IconURL:     iconURL,
			Description: ann[discoveryAnnDescription],
			Href:        href,
			Monitor:     discoveryMonitorEnabled(ann),
		})
	}
	slices.SortFunc(out, func(a, b discoveredService) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

// discoveryMonitorEnabled reports whether ann opts the discovered card into
// an HTTP monitor probe of its href: the native "monitor" flag, or
// homepage's own "ping" name for the same behavior (see discoveryAnnPing).
func discoveryMonitorEnabled(ann map[string]string) bool {
	return ann[discoveryAnnMonitor] == annotationValueTrue || ann[discoveryAnnPing] == annotationValueTrue
}

// discoveryAnnotations reports whether annotations opt into discovery — the
// native prefix's "enabled" annotation is "true", or (when homepageCompat)
// the homepage-compat prefix's is — returning that prefix's annotations with
// the prefix itself stripped. The two annotation sources are never merged:
// whichever prefix's "enabled" flag matched is the one read for every other
// field, so a partially-relabeled Ingress can't end up with fields from both
// conventions.
func discoveryAnnotations(annotations map[string]string, prefix string, homepageCompat bool) (map[string]string, bool) {
	if annotations[prefix+discoveryAnnEnabled] == annotationValueTrue {
		return stripAnnotationPrefix(annotations, prefix), true
	}
	if homepageCompat && annotations[homepageDiscoveryPrefix+discoveryAnnEnabled] == annotationValueTrue {
		return stripAnnotationPrefix(annotations, homepageDiscoveryPrefix), true
	}
	return nil, false
}

func stripAnnotationPrefix(annotations map[string]string, prefix string) map[string]string {
	out := make(map[string]string, len(annotations))
	for k, v := range annotations {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			out[rest] = v
		}
	}
	return out
}

// ingressHref derives a default href from an Ingress with no explicit href
// annotation: the first rule's host, scheme "https" if a TLS entry covers
// that host, otherwise "http". Returns "" when the Ingress has no host-based
// rule to derive one from (e.g. a default-backend-only Ingress), leaving the
// card titled but not linked.
func ingressHref(ing *networkingv1.Ingress) string {
	if len(ing.Spec.Rules) == 0 || ing.Spec.Rules[0].Host == "" {
		return ""
	}
	host := ing.Spec.Rules[0].Host

	scheme := schemeHTTP
	for _, tls := range ing.Spec.TLS {
		if slices.Contains(tls.Hosts, host) {
			scheme = schemeHTTPS
			break
		}
	}
	return scheme + "://" + host + "/"
}

// discoverHTTPRoutes is discoverServices' Gateway API counterpart (gap-
// analysis §4.7): same annotation convention, same discoveredService shape,
// same "no secrets in annotations" constraint — the only difference is the
// source Kind and how a default href is derived. Callers must not invoke
// this unless the cluster is known to have Gateway API installed (a List
// against a nonexistent Kind fails outright, unlike an RBAC-denied one);
// see Poller.GatewayAPIEnabled.
func discoverHTTPRoutes(ctx context.Context, reader client.Reader, namespace string, extraReader client.Reader, extraNamespaces []string, spec pagev1alpha1.DiscoverySpec) ([]discoveredService, error) {
	var routes gatewayv1.HTTPRouteList
	if err := reader.List(ctx, &routes, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing HTTPRoutes: %w", err)
	}
	// See discoverServices' matching loop: a bad extra namespace degrades
	// that namespace only, rather than failing (and pruning) discovery
	// cluster-wide.
	for _, ns := range extraNamespaces {
		var extra gatewayv1.HTTPRouteList
		if err := extraReader.List(ctx, &extra, client.InNamespace(ns)); err != nil {
			pollerLog.Error(err, "discovering HTTPRoutes in extra namespace", "namespace", ns)
			continue
		}
		routes.Items = append(routes.Items, extra.Items...)
	}

	prefix := defaultDiscoveryPrefix
	if spec.AnnotationPrefix != nil && *spec.AnnotationPrefix != "" {
		prefix = *spec.AnnotationPrefix
	}
	homepageCompat := spec.HomepageCompat != nil && *spec.HomepageCompat

	out := make([]discoveredService, 0, len(routes.Items))
	for _, route := range routes.Items {
		ann, ok := discoveryAnnotations(route.Annotations, prefix, homepageCompat)
		if !ok {
			continue
		}

		name := cmp.Or(ann[discoveryAnnName], route.Name)
		group := cmp.Or(ann[discoveryAnnGroup], defaultDiscoveryGroup)
		href := cmp.Or(ann[discoveryAnnHref], httpRouteHref(&route))

		var iconURL string
		if icon := ann[discoveryAnnIcon]; icon != "" {
			iconURL = IconURL(&icon)
		}

		out = append(out, discoveredService{
			Key:         "discovery/httproute/" + route.Namespace + "/" + route.Name,
			Group:       group,
			Name:        name,
			IconURL:     iconURL,
			Description: ann[discoveryAnnDescription],
			Href:        href,
			Monitor:     discoveryMonitorEnabled(ann),
		})
	}
	slices.SortFunc(out, func(a, b discoveredService) int { return strings.Compare(a.Key, b.Key) })
	return out, nil
}

// httpRouteHref derives a default href from an HTTPRoute with no explicit
// href annotation: the first hostname. Unlike ingressHref, there's no TLS
// entry on the HTTPRoute itself to check — TLS termination is the attaching
// Gateway's concern, not the route's — so this always assumes "https",
// matching how Gateway API is predominantly deployed for anything meant to
// be discoverable. Returns "" when the HTTPRoute declares no hostname,
// leaving the card titled but not linked.
func httpRouteHref(route *gatewayv1.HTTPRoute) string {
	if len(route.Spec.Hostnames) == 0 {
		return ""
	}
	return "https://" + string(route.Spec.Hostnames[0]) + "/"
}

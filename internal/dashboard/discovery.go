package dashboard

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
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
	discoveryAnnPing        = "ping"
)

// defaultDiscoveryGroup is the Group a discovered card renders under when
// its Ingress sets no group annotation.
const defaultDiscoveryGroup = "Discovered"

// discoveredService is a service card synthesized from an annotated Ingress,
// good for exactly what an Ingress annotation can safely carry: title/icon/
// description/href/ping. Never a polled widget — see DiscoverySpec's doc
// comment on why annotations can't carry secrets.
type discoveredService struct {
	Key         string
	Group       string
	Name        string
	IconURL     string
	Description string
	Href        string
	Ping        bool
}

// discoverServices lists every Ingress in namespace and returns the ones
// that opt into discovery via annotation, per spec's AnnotationPrefix/
// HomepageCompat.
func discoverServices(ctx context.Context, reader client.Reader, namespace string, spec pagev1alpha1.DiscoverySpec) ([]discoveredService, error) {
	var ingresses networkingv1.IngressList
	if err := reader.List(ctx, &ingresses, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing Ingresses: %w", err)
	}

	prefix := defaultDiscoveryPrefix
	if spec.AnnotationPrefix != nil && *spec.AnnotationPrefix != "" {
		prefix = *spec.AnnotationPrefix
	}
	homepageCompat := spec.HomepageCompat != nil && *spec.HomepageCompat == pagev1alpha1.Enabled

	out := make([]discoveredService, 0, len(ingresses.Items))
	for _, ing := range ingresses.Items {
		ann, ok := discoveryAnnotations(ing.Annotations, prefix, homepageCompat)
		if !ok {
			continue
		}

		name := ann[discoveryAnnName]
		if name == "" {
			name = ing.Name
		}
		group := ann[discoveryAnnGroup]
		if group == "" {
			group = defaultDiscoveryGroup
		}
		href := ann[discoveryAnnHref]
		if href == "" {
			href = ingressHref(&ing)
		}

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
			Ping:        ann[discoveryAnnPing] == "true",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// discoveryAnnotations reports whether annotations opt into discovery — the
// native prefix's "enabled" annotation is "true", or (when homepageCompat)
// the homepage-compat prefix's is — returning that prefix's annotations with
// the prefix itself stripped. The two annotation sources are never merged:
// whichever prefix's "enabled" flag matched is the one read for every other
// field, so a partially-relabeled Ingress can't end up with fields from both
// conventions.
func discoveryAnnotations(annotations map[string]string, prefix string, homepageCompat bool) (map[string]string, bool) {
	if annotations[prefix+discoveryAnnEnabled] == "true" {
		return stripAnnotationPrefix(annotations, prefix), true
	}
	if homepageCompat && annotations[homepageDiscoveryPrefix+discoveryAnnEnabled] == "true" {
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

	scheme := "http"
	for _, tls := range ing.Spec.TLS {
		if slices.Contains(tls.Hosts, host) {
			scheme = "https"
			break
		}
	}
	return scheme + "://" + host + "/"
}

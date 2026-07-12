package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// managedAnnotationsKey records, on the Service/Ingress/HTTPRoute this
// controller owns, exactly which annotation keys it last set from
// spec.service.annotations/spec.ingress.annotations/spec.gateway.annotations.
// mergeManagedAnnotations uses it to prune a key that's since been removed
// from the spec without touching any annotation a different controller (a
// cloud LB controller, an ingress controller, cert-manager, ...) wrote onto
// the same object — a wholesale found.Annotations = desired.Annotations
// would otherwise clobber those on every drift-triggering reconcile.
const managedAnnotationsKey = "page.kubepage.dev/managed-annotations"

// mergeManagedAnnotations returns existing with desired's keys set, any key
// this controller previously recorded as managed but that's no longer in
// desired removed, and every other (foreign) key left untouched.
func mergeManagedAnnotations(existing, desired map[string]string) map[string]string {
	merged := maps.Clone(existing)
	if merged == nil {
		merged = map[string]string{}
	}

	for k := range strings.SplitSeq(merged[managedAnnotationsKey], ",") {
		if k == "" {
			continue
		}
		if _, stillDesired := desired[k]; !stillDesired {
			delete(merged, k)
		}
	}
	maps.Copy(merged, desired)

	if len(desired) == 0 {
		delete(merged, managedAnnotationsKey)
		return merged
	}
	keys := slices.Sorted(maps.Keys(desired))
	merged[managedAnnotationsKey] = strings.Join(keys, ",")
	return merged
}

// serviceForDashboard returns the ClusterIP Service fronting instance's
// dashboard pods, owned by instance.
func (r *DashboardReconciler) serviceForDashboard(instance *pagev1alpha1.Dashboard) (*corev1.Service, error) {
	ports := []corev1.ServicePort{
		{
			Name:       instanceContainerName,
			Port:       instance.Spec.ContainerPort,
			TargetPort: intstr.FromInt32(instance.Spec.ContainerPort),
		},
	}
	// The metrics port is opt-in on the Service (spec.metrics.enabled,
	// default off): the dashboard's /metrics has no authn/authz of its own
	// (unlike the manager's), so exposing it on the ClusterIP Service by
	// default would let any pod in the cluster read per-service up/down
	// status and poll metrics with no RBAC check. It's never referenced by
	// reconcileIngress/reconcileHTTPRoute either way — see
	// dashboardMetricsPort's doc comment — and pod-level scraping (e.g. a
	// PodMonitor targeting the pod IP directly) keeps working regardless of
	// this setting.
	if instance.Spec.Metrics != nil && instance.Spec.Metrics.Enabled == pagev1alpha1.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       dashboardMetricsPortName,
			Port:       dashboardMetricsPort,
			TargetPort: intstr.FromInt32(dashboardMetricsPort),
		})
	}

	svcType := corev1.ServiceTypeClusterIP
	var annotations map[string]string
	if instance.Spec.Service != nil {
		if instance.Spec.Service.Type != "" {
			svcType = corev1.ServiceType(instance.Spec.Service.Type)
		}
		annotations = instance.Spec.Service.Annotations
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        instance.Name,
			Namespace:   instance.Namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: selectorLabelsForDashboard(),
			Ports:    ports,
		},
	}
	if err := ctrl.SetControllerReference(instance, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

// reconcileService ensures the dashboard Service for instance exists and
// matches the desired state. Every Dashboard gets a Service (it's how its
// Deployment is reached at all, Ingress or not), so unlike Ingress there's
// no enabled/disabled toggle here.
func (r *DashboardReconciler) reconcileService(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	desired, err := r.serviceForDashboard(instance)
	if err != nil {
		return fmt.Errorf("defining Service: %w", err)
	}

	found := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating a new Service", "Service.Namespace", desired.Namespace, "Service.Name", desired.Name)
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating Service %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("getting Service %s/%s: %w", desired.Namespace, desired.Name, err)
	}

	clusterIP := found.Spec.ClusterIP
	mergedAnnotations := mergeManagedAnnotations(found.Annotations, desired.Annotations)
	if !portsEqual(found.Spec.Ports, desired.Spec.Ports) || !maps.Equal(found.Spec.Selector, desired.Spec.Selector) ||
		found.Spec.Type != desired.Spec.Type || !maps.Equal(found.Annotations, mergedAnnotations) {
		found.Spec.Ports = desired.Spec.Ports
		found.Spec.Selector = desired.Spec.Selector
		found.Spec.Type = desired.Spec.Type
		found.Annotations = mergedAnnotations
		found.Spec.ClusterIP = clusterIP // immutable once set; preserve it
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("updating Service %s/%s: %w", found.Namespace, found.Name, err)
		}
	}
	return nil
}

// dashboardURL derives where instance is reachable, for
// status.url — see DashboardStatus.URL's doc comment for the precedence
// order and its LoadBalancer-external-IP non-goal.
func dashboardURL(instance *pagev1alpha1.Dashboard) string {
	if ing := instance.Spec.Ingress; ing != nil && ing.Enabled == pagev1alpha1.Enabled {
		scheme := "http"
		if ing.TLS != nil {
			scheme = "https"
		}
		return scheme + "://" + ing.Host + "/"
	}
	if gw := instance.Spec.Gateway; gw != nil && gw.Enabled == pagev1alpha1.Enabled && len(gw.Hostnames) > 0 {
		return "https://" + gw.Hostnames[0] + "/"
	}
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/", instance.Name, instance.Namespace, instance.Spec.ContainerPort)
}

// ingressForDashboard returns the Ingress exposing instance's Service at
// spec.ingress.host, owned by instance. Only called when spec.ingress is set
// and enabled.
func (r *DashboardReconciler) ingressForDashboard(instance *pagev1alpha1.Dashboard) (*networkingv1.Ingress, error) {
	ing := instance.Spec.Ingress
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        instance.Name,
			Namespace:   instance.Namespace,
			Annotations: ing.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ing.IngressClassName,
			Rules: []networkingv1.IngressRule{{
				Host: ing.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: instance.Name,
									Port: networkingv1.ServiceBackendPort{Number: instance.Spec.ContainerPort},
								},
							},
						}},
					},
				},
			}},
		},
	}
	if ing.TLS != nil {
		ingress.Spec.TLS = []networkingv1.IngressTLS{{
			Hosts:      []string{ing.Host},
			SecretName: ing.TLS.SecretName,
		}}
	}

	if err := ctrl.SetControllerReference(instance, ingress, r.Scheme); err != nil {
		return nil, err
	}
	return ingress, nil
}

// reconcileIngress ensures the Ingress for instance matches spec.ingress: an
// owned Ingress is created/updated when enabled, and removed if it exists
// but the user has since disabled or removed spec.ingress (so toggling it
// off actually takes the Ingress down rather than leaving a stale one).
func (r *DashboardReconciler) reconcileIngress(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	enabled := instance.Spec.Ingress != nil && instance.Spec.Ingress.Enabled == pagev1alpha1.Enabled

	found := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		if !enabled {
			return nil
		}
		desired, err := r.ingressForDashboard(instance)
		if err != nil {
			return fmt.Errorf("defining Ingress: %w", err)
		}
		log.Info("Creating a new Ingress", "Ingress.Namespace", desired.Namespace, "Ingress.Name", desired.Name)
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating Ingress %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("getting Ingress %s/%s: %w", instance.Namespace, instance.Name, err)
	}

	if !enabled {
		log.Info("Deleting Ingress: spec.ingress.enabled is now false", "Ingress.Namespace", found.Namespace, "Ingress.Name", found.Name)
		if err := r.Delete(ctx, found); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting Ingress %s/%s: %w", found.Namespace, found.Name, err)
		}
		return nil
	}

	desired, err := r.ingressForDashboard(instance)
	if err != nil {
		return fmt.Errorf("defining Ingress: %w", err)
	}
	mergedAnnotations := mergeManagedAnnotations(found.Annotations, desired.Annotations)
	if !ingressSpecsEqual(found.Spec, desired.Spec) || !maps.Equal(found.Annotations, mergedAnnotations) {
		found.Spec = desired.Spec
		found.Annotations = mergedAnnotations
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("updating Ingress %s/%s: %w", found.Namespace, found.Name, err)
		}
	}
	return nil
}

// portsEqual compares two ServicePort slices field-by-field (not via
// DeepEqual against the API-server-defaulted stored object, same reasoning
// as the Deployment drift check in reconcileDeployment).
func portsEqual(a, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Port != b[i].Port || a[i].TargetPort != b[i].TargetPort {
			return false
		}
	}
	return true
}

// ingressSpecsEqual compares the fields of IngressSpec this controller
// actually sets, ignoring any the API server might default.
func ingressSpecsEqual(a, b networkingv1.IngressSpec) bool {
	if !equalStringPtr(a.IngressClassName, b.IngressClassName) {
		return false
	}
	if len(a.Rules) != len(b.Rules) || len(a.TLS) != len(b.TLS) {
		return false
	}
	for i := range a.Rules {
		ar, br := a.Rules[i], b.Rules[i]
		if ar.Host != br.Host {
			return false
		}
		if ar.HTTP == nil || br.HTTP == nil || len(ar.HTTP.Paths) != len(br.HTTP.Paths) {
			return false
		}
		for j := range ar.HTTP.Paths {
			ap, bp := ar.HTTP.Paths[j], br.HTTP.Paths[j]
			if ap.Path != bp.Path {
				return false
			}
			if ap.Backend.Service == nil || bp.Backend.Service == nil {
				return false
			}
			if ap.Backend.Service.Name != bp.Backend.Service.Name || ap.Backend.Service.Port.Number != bp.Backend.Service.Port.Number {
				return false
			}
		}
	}
	for i := range a.TLS {
		if a.TLS[i].SecretName != b.TLS[i].SecretName || len(a.TLS[i].Hosts) != len(b.TLS[i].Hosts) {
			return false
		}
		for j := range a.TLS[i].Hosts {
			if a.TLS[i].Hosts[j] != b.TLS[i].Hosts[j] {
				return false
			}
		}
	}
	return true
}

func equalStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// errGatewayAPINotInstalled is returned by reconcileHTTPRoute when
// spec.gateway is enabled but the cluster has no Gateway API CRDs — surfaced
// as a clear Available=False status condition rather than the manager
// crashing trying to watch a Kind that doesn't exist (see
// GatewayAPIEnabled's doc comment on DashboardReconciler).
var errGatewayAPINotInstalled = errors.New("spec.gateway is enabled but Gateway API CRDs are not installed in this cluster")

// httpRouteForDashboard returns the HTTPRoute exposing instance's Service at
// spec.gateway's hostnames/parentRef, owned by instance. Only called when
// spec.gateway is set and enabled.
func (r *DashboardReconciler) httpRouteForDashboard(instance *pagev1alpha1.Dashboard) (*gatewayv1.HTTPRoute, error) {
	gw := instance.Spec.Gateway

	hostnames := make([]gatewayv1.Hostname, 0, len(gw.Hostnames))
	for _, h := range gw.Hostnames {
		hostnames = append(hostnames, gatewayv1.Hostname(h))
	}

	var parentNamespace *gatewayv1.Namespace
	if gw.ParentRef.Namespace != nil {
		ns := gatewayv1.Namespace(*gw.ParentRef.Namespace)
		parentNamespace = &ns
	}
	var sectionName *gatewayv1.SectionName
	if gw.ParentRef.SectionName != nil {
		sn := gatewayv1.SectionName(*gw.ParentRef.SectionName)
		sectionName = &sn
	}
	port := instance.Spec.ContainerPort

	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:        instance.Name,
			Namespace:   instance.Namespace,
			Annotations: gw.Annotations,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name:        gatewayv1.ObjectName(gw.ParentRef.Name),
					Namespace:   parentNamespace,
					SectionName: sectionName,
				}},
			},
			Hostnames: hostnames,
			Rules: []gatewayv1.HTTPRouteRule{{
				BackendRefs: []gatewayv1.HTTPBackendRef{{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName(instance.Name),
							Port: &port,
						},
					},
				}},
			}},
		},
	}

	if err := ctrl.SetControllerReference(instance, route, r.Scheme); err != nil {
		return nil, err
	}
	return route, nil
}

// reconcileHTTPRoute ensures the HTTPRoute for instance matches
// spec.gateway: an owned HTTPRoute is created/updated when enabled, and
// removed if it exists but the user has since disabled or removed
// spec.gateway — mirroring reconcileIngress. Gated on r.GatewayAPIEnabled
// (checked once at manager startup): if spec.gateway is enabled but the
// cluster has no Gateway API CRDs, returns errGatewayAPINotInstalled instead
// of attempting a Get/Create that would otherwise fail with a confusing
// "no matches for kind" error.
func (r *DashboardReconciler) reconcileHTTPRoute(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	enabled := instance.Spec.Gateway != nil && instance.Spec.Gateway.Enabled == pagev1alpha1.Enabled
	if !enabled {
		if !r.GatewayAPIEnabled {
			// Nothing to do, and nothing we could even look up.
			return nil
		}
		found := &gatewayv1.HTTPRoute{}
		err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return fmt.Errorf("getting HTTPRoute %s/%s: %w", instance.Namespace, instance.Name, err)
		}
		log.Info("Deleting HTTPRoute: spec.gateway.enabled is now false", "HTTPRoute.Namespace", found.Namespace, "HTTPRoute.Name", found.Name)
		if err := r.Delete(ctx, found); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting HTTPRoute %s/%s: %w", found.Namespace, found.Name, err)
		}
		return nil
	}

	if !r.GatewayAPIEnabled {
		return errGatewayAPINotInstalled
	}

	desired, err := r.httpRouteForDashboard(instance)
	if err != nil {
		return fmt.Errorf("defining HTTPRoute: %w", err)
	}

	found := &gatewayv1.HTTPRoute{}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating a new HTTPRoute", "HTTPRoute.Namespace", desired.Namespace, "HTTPRoute.Name", desired.Name)
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating HTTPRoute %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("getting HTTPRoute %s/%s: %w", instance.Namespace, instance.Name, err)
	}

	mergedAnnotations := mergeManagedAnnotations(found.Annotations, desired.Annotations)
	if !httpRouteSpecsEqual(found.Spec, desired.Spec) || !maps.Equal(found.Annotations, mergedAnnotations) {
		found.Spec = desired.Spec
		found.Annotations = mergedAnnotations
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("updating HTTPRoute %s/%s: %w", found.Namespace, found.Name, err)
		}
	}
	return nil
}

// httpRouteSpecsEqual compares the fields of HTTPRouteSpec this controller
// actually sets, ignoring any the API server might default (ParentReference
// and BackendObjectReference both carry +kubebuilder:default Group/Kind/
// Weight values that a bare struct literal never has, same reasoning as
// ingressSpecsEqual above).
func httpRouteSpecsEqual(a, b gatewayv1.HTTPRouteSpec) bool {
	if len(a.ParentRefs) != len(b.ParentRefs) || len(a.Hostnames) != len(b.Hostnames) || len(a.Rules) != len(b.Rules) {
		return false
	}
	for i := range a.ParentRefs {
		ap, bp := a.ParentRefs[i], b.ParentRefs[i]
		if ap.Name != bp.Name {
			return false
		}
		if !equalGatewayNamespacePtr(ap.Namespace, bp.Namespace) || !equalGatewaySectionNamePtr(ap.SectionName, bp.SectionName) {
			return false
		}
	}
	for i := range a.Hostnames {
		if a.Hostnames[i] != b.Hostnames[i] {
			return false
		}
	}
	for i := range a.Rules {
		ar, br := a.Rules[i], b.Rules[i]
		if len(ar.BackendRefs) != len(br.BackendRefs) {
			return false
		}
		for j := range ar.BackendRefs {
			abr, bbr := ar.BackendRefs[j].BackendRef, br.BackendRefs[j].BackendRef
			if abr.Name != bbr.Name {
				return false
			}
			if (abr.Port == nil) != (bbr.Port == nil) {
				return false
			}
			if abr.Port != nil && bbr.Port != nil && *abr.Port != *bbr.Port {
				return false
			}
		}
	}
	return true
}

func equalGatewayNamespacePtr(a, b *gatewayv1.Namespace) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func equalGatewaySectionNamePtr(a, b *gatewayv1.SectionName) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// dnsPort/apiServerPort are the fixed ports networkPolicyForDashboard always
// allows egress to once egress is restricted (spec.networkPolicy.egressCIDRs
// non-empty) — see networkPolicyForDashboard's doc comment for why neither
// rule restricts the destination peer.
const (
	dnsPort       = 53
	apiServerPort = 443
)

// networkPolicyForDashboard returns the NetworkPolicy scoping instance's
// dashboard pods, owned by instance. Only called when spec.networkPolicy is
// set and enabled.
//
// Ingress: the dashboard's containerPort is opened to
// spec.networkPolicy.ingressNamespaceSelector (any namespace when unset,
// matching the Service's own unrestricted-by-default reach); the metrics
// port is opened the same way, scoped by metricsNamespaceSelector, but only
// when spec.metrics.enabled — there's nothing to protect on that port
// otherwise, since the Service doesn't expose it either (see
// serviceForDashboard).
//
// Egress: left unrestricted (no Egress policyType) unless
// spec.networkPolicy.egressCIDRs is set, matching the "default allow-all
// egress to stay non-breaking" design in the security review this field
// implements — widget URLs are CRD-supplied by design (SECURITY.md's
// explicit non-goals), so this is an opt-in positive scope, not a default
// deny. When egressCIDRs is set, DNS (port 53) and HTTPS (port 443, which
// covers the Kubernetes API server) are allowed to any destination: pinning
// either to a specific peer would require guessing the cluster's DNS
// Service or API server address, which varies too much across
// CNIs/providers to hardcode safely, and both need to keep working
// regardless of which upstreams an operator lists — only the
// operator-supplied CIDRs get a real destination restriction.
func (r *DashboardReconciler) networkPolicyForDashboard(instance *pagev1alpha1.Dashboard) (*networkingv1.NetworkPolicy, error) {
	np := instance.Spec.NetworkPolicy

	ingressRules := []networkingv1.NetworkPolicyIngressRule{{
		Ports: []networkingv1.NetworkPolicyPort{{Protocol: new(corev1.ProtocolTCP), Port: new(intstr.FromInt32(instance.Spec.ContainerPort))}},
		From:  namespaceSelectorPeers(np.IngressNamespaceSelector),
	}}
	if instance.Spec.Metrics != nil && instance.Spec.Metrics.Enabled == pagev1alpha1.Enabled {
		ingressRules = append(ingressRules, networkingv1.NetworkPolicyIngressRule{
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: new(corev1.ProtocolTCP), Port: new(intstr.FromInt32(dashboardMetricsPort))}},
			From:  namespaceSelectorPeers(np.MetricsNamespaceSelector),
		})
	}

	policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	var egressRules []networkingv1.NetworkPolicyEgressRule
	if len(np.EgressCIDRs) > 0 {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
		egressRules = append(egressRules,
			networkingv1.NetworkPolicyEgressRule{
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: new(corev1.ProtocolUDP), Port: new(intstr.FromInt32(dnsPort))},
					{Protocol: new(corev1.ProtocolTCP), Port: new(intstr.FromInt32(dnsPort))},
				},
			},
			networkingv1.NetworkPolicyEgressRule{
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: new(corev1.ProtocolTCP), Port: new(intstr.FromInt32(apiServerPort))},
				},
			},
		)
		for _, cidr := range np.EgressCIDRs {
			egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
				To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr}}},
			})
		}
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selectorLabelsForDashboard()},
			PolicyTypes: policyTypes,
			Ingress:     ingressRules,
			Egress:      egressRules,
		},
	}
	if err := ctrl.SetControllerReference(instance, policy, r.Scheme); err != nil {
		return nil, err
	}
	return policy, nil
}

// namespaceSelectorPeers returns the NetworkPolicyPeer list scoping an
// ingress rule to selector's namespaces, or nil (no restriction, "from
// anywhere") when selector is unset — see networkPolicyForDashboard's doc
// comment.
func namespaceSelectorPeers(selector *metav1.LabelSelector) []networkingv1.NetworkPolicyPeer {
	if selector == nil {
		return nil
	}
	return []networkingv1.NetworkPolicyPeer{{NamespaceSelector: selector}}
}

// reconcileNetworkPolicy ensures the NetworkPolicy for instance matches
// spec.networkPolicy: an owned NetworkPolicy is created/updated when
// enabled, and removed if it exists but the user has since disabled or
// removed spec.networkPolicy — mirroring reconcileIngress/reconcileHTTPRoute.
//
// Unlike those two, this deliberately skips the Get entirely (and so never
// detects a toggle-off needing cleanup) when spec.networkPolicy was never
// set: least-privilege in practice, not just in the RBAC grant — a Dashboard
// that never opts into this field causes this reconciler to touch the
// NetworkPolicy API not at all, rather than relying solely on the ClusterRole
// to stop it from doing anything. Toggling spec.networkPolicy from set back
// to unset leaves a stale NetworkPolicy object behind (still cleaned up when
// the Dashboard itself is deleted, since it's owner-referenced) as the
// accepted trade-off.
func (r *DashboardReconciler) reconcileNetworkPolicy(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	enabled := instance.Spec.NetworkPolicy != nil && instance.Spec.NetworkPolicy.Enabled == pagev1alpha1.Enabled
	if !enabled {
		return nil
	}

	found := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		desired, err := r.networkPolicyForDashboard(instance)
		if err != nil {
			return fmt.Errorf("defining NetworkPolicy: %w", err)
		}
		log.Info("Creating a new NetworkPolicy", "NetworkPolicy.Namespace", desired.Namespace, "NetworkPolicy.Name", desired.Name)
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating NetworkPolicy %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("getting NetworkPolicy %s/%s: %w", instance.Namespace, instance.Name, err)
	}

	desired, err := r.networkPolicyForDashboard(instance)
	if err != nil {
		return fmt.Errorf("defining NetworkPolicy: %w", err)
	}
	if !reflect.DeepEqual(found.Spec, desired.Spec) {
		found.Spec = desired.Spec
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("updating NetworkPolicy %s/%s: %w", found.Namespace, found.Name, err)
		}
	}
	return nil
}

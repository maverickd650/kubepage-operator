package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// kubeMetricsWidgetType is the InfoWidget.Spec.Type whose dashboard widget
// (internal/dashboard/kubemetrics.go) reads cluster-scoped nodes and
// metrics.k8s.io; its presence is what gates the cluster RBAC below.
const kubeMetricsWidgetType = "kubemetrics"

// RBAC verbs shared by the rule sets below, pulled into constants so goconst
// doesn't flag the repeated literals across dashboardRules/clusterMetricsRules.
const (
	verbGet   = "get"
	verbList  = "list"
	verbWatch = "watch"

	resourcePods    = "pods"
	resourceSecrets = "secrets"

	// secretAllowWidgetsValue is the value SecretAllowWidgetsLabel must be
	// set to for filterLabeledSecrets to treat a Secret as widget-readable.
	secretAllowWidgetsValue = "true"
)

// clusterMetricsRules are the cluster-scoped permissions the dashboard pod
// needs when it renders a kubemetrics InfoWidget: read nodes (for capacity)
// and node metrics from metrics-server (for live usage). Granted via a
// ClusterRole rather than the per-Dashboard Role because both resources are
// cluster-scoped.
var clusterMetricsRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{""},
		Resources: []string{"nodes"},
		Verbs:     []string{verbGet, verbList},
	},
	{
		APIGroups: []string{"metrics.k8s.io"},
		Resources: []string{"nodes"},
		Verbs:     []string{verbGet, verbList},
	},
}

// dashboardConfigRule is the read access the dashboard pod needs in its
// Dashboard's own namespace to the config CRDs it renders (internal/dashboard's
// LoadSite and Poller, both cache-backed Lists). Namespace-scoped rather than
// reusing the manager's own cluster-wide ClusterRole: the dashboard pod only
// ever needs its own namespace, and granting it the manager's full
// cluster-wide permissions would be a privilege escalation for no functional
// benefit.
var dashboardConfigRule = rbacv1.PolicyRule{
	APIGroups: []string{pagev1alpha1.GroupVersion.Group},
	Resources: []string{"dashboards", "dashboardstyles", "servicecards", "bookmarks", "infowidgets"},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardIngressRule is the read access the dashboard pod needs to
// synthesize service cards from annotated Ingresses (internal/dashboard/
// discovery.go) when the Dashboard's DiscoverySpec is enabled. Added to the
// per-Dashboard Role only while discovery is on (see dashboardRoles), the
// same "grant only while it's actually used" treatment as the cluster
// metrics ClusterRole below — Ingresses can carry no secrets themselves, but
// they can reveal internal hostnames/paths a least-privileged dashboard pod
// has no other reason to read.
var dashboardIngressRule = rbacv1.PolicyRule{
	APIGroups: []string{"networking.k8s.io"},
	Resources: []string{"ingresses"},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardHTTPRouteRule mirrors dashboardIngressRule for Gateway API
// HTTPRoutes (internal/dashboard/discovery.go's HTTPRoute discovery, the
// gap-analysis §4.7 fast-follow to Ingress discovery). Added to the
// per-Dashboard Role only while discovery is on *and* the cluster actually
// has Gateway API installed (dashboardRoles' gatewayAPIEnabled parameter,
// sourced from DashboardReconciler.GatewayAPIEnabled) — granting RBAC on a
// Kind the apiserver doesn't recognize is harmless on its own, but there's
// no reason to grant it when the dashboard pod could never use it.
var dashboardHTTPRouteRule = rbacv1.PolicyRule{
	APIGroups: []string{"gateway.networking.k8s.io"},
	Resources: []string{"httproutes"},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardPodsRule is the read access the dashboard pod needs in its own
// namespace to evaluate a ServiceCard's PodSelector
// (internal/dashboard/poller.go's monitor): listing/watching pods to compute
// "M/N ready" status. Unlike the kubemetrics ClusterRole below, this is
// namespace-scoped and owner-referenced like the rest of the per-Dashboard
// Role, so it's granted unconditionally rather than added/removed as
// PodSelector usage comes and goes — there's no GC/finalizer cost to it.
var dashboardPodsRule = rbacv1.PolicyRule{
	APIGroups: []string{""},
	Resources: []string{resourcePods},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardRoles builds the per-Dashboard Role's rules: always read access to
// the config CRDs (dashboardConfigRule) and to Pods (dashboardPodsRule);
// Ingresses (and, when gatewayAPIEnabled, HTTPRoutes) while discoveryEnabled;
// plus get access to exactly the Secrets referenced by the Dashboard's
// widgets (secretNames, already sorted and de-duplicated). The Secret rule
// is scoped with resourceNames and limited to get: the Poller only ever Gets
// a referenced Secret (internal/dashboard/poller.go resolveSecret, via a
// separate uncached client), and RBAC resourceNames can't restrict
// list/watch — so the dashboard pod can read only the credentials it
// actually uses, not every Secret in the namespace. When nothing references
// a Secret the rule is omitted entirely, since an empty resourceNames would
// instead mean "all Secrets" — the opposite of the intent.
//
// Trust model note: this scoping protects against the dashboard pod itself
// being compromised (e.g. a malicious upstream response), not against a
// malicious ServiceCard/InfoWidget author. Whoever can create a
// ServiceCard/InfoWidget in this namespace can name *any* Secret in it via
// secretKeyRef (referencedSecretNames below has no allowlist) and point the
// widget's own url at a server they control, which the resolved plaintext is
// then sent to (as e.g. a Bearer header) — an effective read of that
// Secret's contents without ever needing "get secrets" RBAC directly.
// Anyone who can create these CRDs in a namespace should therefore be
// treated as trusted with every Secret in it, the same as anyone who can
// create a Pod mounting arbitrary Secret volumes.
func dashboardRoles(secretNames []string, discoveryEnabled, gatewayAPIEnabled bool) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{dashboardConfigRule, dashboardPodsRule}
	if discoveryEnabled {
		rules = append(rules, dashboardIngressRule)
		if gatewayAPIEnabled {
			rules = append(rules, dashboardHTTPRouteRule)
		}
	}
	if len(secretNames) > 0 {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{resourceSecrets},
			Verbs:         []string{verbGet},
			ResourceNames: secretNames,
		})
	}
	return rules
}

// referencedSecretNames returns the sorted, de-duplicated set of Secret names
// that ServiceCards and InfoWidgets bound to instance reference via a
// secretKeyRef, plus any referenced from instance.Spec.WidgetDefaults (the
// per-widget-type shared secret defaults — see DashboardSpec.WidgetDefaults'
// doc comment). It's what scopes the dashboard pod's Secret access (see
// dashboardRoles); the DashboardReconciler already re-reconciles on
// ServiceCard/InfoWidget changes (SetupWithManager Watches) and on the
// Dashboard's own spec (the normal reconcile trigger), so the Role's scoped
// list stays in sync as widgets or widgetDefaults add or drop credential
// refs.
//
// Deliberately unfiltered: any Secret name a bound ServiceCard/InfoWidget or
// instance.Spec.WidgetDefaults references is included, with no allowlist of
// which Secrets a widget "may" use — see dashboardRoles' trust-model note for
// what that implies about who should be allowed to create these CRDs (or, for
// widgetDefaults, edit the Dashboard itself).
func (r *DashboardReconciler) referencedSecretNames(ctx context.Context, instance *pagev1alpha1.Dashboard) ([]string, error) {
	names := map[string]struct{}{}

	var entries pagev1alpha1.ServiceCardList
	if err := r.List(ctx, &entries, client.InNamespace(instance.Namespace)); err != nil {
		return nil, fmt.Errorf("listing ServiceCards: %w", err)
	}
	for _, e := range entries.Items {
		if e.Spec.DashboardRef.Name != instance.Name {
			continue
		}
		for _, entry := range e.Spec.Entries() {
			for _, w := range entry.Widgets {
				for _, src := range w.Secrets {
					if src.SecretKeyRef != nil {
						names[src.SecretKeyRef.Name] = struct{}{}
					}
				}
				if w.CACert != nil && w.CACert.SecretKeyRef != nil {
					names[w.CACert.SecretKeyRef.Name] = struct{}{}
				}
			}
		}
	}

	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return nil, fmt.Errorf("listing InfoWidgets: %w", err)
	}
	for _, w := range infoWidgets.Items {
		if w.Spec.DashboardRef.Name != instance.Name {
			continue
		}
		for _, entry := range w.Spec.Entries() {
			for _, src := range entry.Secrets {
				if src.SecretKeyRef != nil {
					names[src.SecretKeyRef.Name] = struct{}{}
				}
			}
			if entry.CACert != nil && entry.CACert.SecretKeyRef != nil {
				names[entry.CACert.SecretKeyRef.Name] = struct{}{}
			}
		}
	}

	for _, defaults := range instance.Spec.WidgetDefaults {
		for _, src := range defaults.Secrets {
			if src.SecretKeyRef != nil {
				names[src.SecretKeyRef.Name] = struct{}{}
			}
		}
		if defaults.CACert != nil && defaults.CACert.SecretKeyRef != nil {
			names[defaults.CACert.SecretKeyRef.Name] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	slices.Sort(out)

	if instance.Spec.SecretPolicy != nil && *instance.Spec.SecretPolicy == pagev1alpha1.SecretPolicyLabeled {
		return r.filterLabeledSecrets(ctx, instance.Namespace, out)
	}
	return out, nil
}

// filterLabeledSecrets keeps only the Secret names, of those given, that
// carry the page.kubepage.dev/allow-widgets: "true" label — the
// spec.secretPolicy: Labeled opt-in (see DashboardSpec.SecretPolicy's doc
// comment). A name whose Secret doesn't exist (yet, or ever) is dropped
// rather than erroring: the dashboard pod's own Get on it already produces a
// clear "does not exist" card error for that widget, and one missing Secret
// shouldn't block reconciling RBAC for every other widget.
//
// Reads through DirectReader (uncached) rather than this reconciler's own
// (cache-backed) Client deliberately: an informer Get on Secret would start a
// cluster-wide Secret watch/cache on the manager, holding every Secret's
// plaintext content in memory for the process lifetime — exactly what this
// project avoids for the dashboard pod (see poller.go's resolveSecret) and
// shouldn't introduce for the manager either, even though the manager
// already holds "secrets get" RBAC cluster-wide (see SECURITY.md's P2.3
// trade-off note) to provision the per-Dashboard Role below.
func (r *DashboardReconciler) filterLabeledSecrets(ctx context.Context, namespace string, names []string) ([]string, error) {
	allowed := make([]string, 0, len(names))
	for _, name := range names {
		secret := &corev1.Secret{}
		err := r.DirectReader.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
		switch {
		case apierrors.IsNotFound(err):
			continue
		case err != nil:
			return nil, fmt.Errorf("getting Secret %q to check secretPolicy label: %w", name, err)
		}
		if secret.Labels[pagev1alpha1.SecretAllowWidgetsLabel] == secretAllowWidgetsValue {
			allowed = append(allowed, name)
		}
	}
	return allowed, nil
}

// authSecretNames returns instance.Spec.Auth's basicAuthSecretRef Secret
// name, if set, as a single-element (or empty) slice. Deliberately not
// subject to referencedSecretNames' secretPolicy: Labeled filtering — that
// gate protects against a ServiceCard/InfoWidget *author* exfiltrating an
// arbitrary Secret via a widget's own URL (see dashboardRoles' trust-model
// note); the auth Secret is set directly on the Dashboard spec by whoever
// already controls the Dashboard, the same trust level as every other
// Dashboard-spec field, not a widget-supplied reference.
func authSecretNames(instance *pagev1alpha1.Dashboard) []string {
	if instance.Spec.Auth == nil || instance.Spec.Auth.BasicAuthSecretRef == nil {
		return nil
	}
	return []string{instance.Spec.Auth.BasicAuthSecretRef.Name}
}

// reconcileDashboardRBAC ensures the per-Dashboard ServiceAccount, Role, and
// RoleBinding the dashboard pod runs as. All three are named after instance
// and owned by it, so they're garbage-collected along with everything else
// when the Dashboard is deleted.
func (r *DashboardReconciler) reconcileDashboardRBAC(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	if err := r.reconcileServiceAccount(ctx, instance); err != nil {
		return fmt.Errorf("reconciling ServiceAccount: %w", err)
	}
	if err := r.reconcileRole(ctx, instance); err != nil {
		return fmt.Errorf("reconciling Role: %w", err)
	}
	if err := r.reconcileRoleBinding(ctx, instance); err != nil {
		return fmt.Errorf("reconciling RoleBinding: %w", err)
	}
	return nil
}

func (r *DashboardReconciler) reconcileServiceAccount(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
	}
	if err := ctrl.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, found)
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new ServiceAccount", "ServiceAccount.Namespace", desired.Namespace, "ServiceAccount.Name", desired.Name)
		return r.Create(ctx, desired)
	}
	return err
}

func (r *DashboardReconciler) reconcileRole(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	secretNames, err := r.referencedSecretNames(ctx, instance)
	if err != nil {
		return err
	}
	if auth := authSecretNames(instance); len(auth) > 0 {
		secretNames = append(secretNames, auth...)
		slices.Sort(secretNames)
		secretNames = slices.Compact(secretNames)
	}

	discoveryEnabled := instance.Spec.Discovery != nil && instance.Spec.Discovery.Enabled == pagev1alpha1.Enabled
	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Rules:      dashboardRoles(secretNames, discoveryEnabled, r.GatewayAPIEnabled),
	}
	if err := ctrl.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating a new Role", "Role.Namespace", desired.Namespace, "Role.Name", desired.Name)
		return r.Create(ctx, desired)
	case err != nil:
		return err
	case !policyRulesEqual(found.Rules, desired.Rules):
		found.Rules = desired.Rules
		return r.Update(ctx, found)
	}
	return nil
}

func (r *DashboardReconciler) reconcileRoleBinding(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	desired := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     instance.Name,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      instance.Name,
			Namespace: instance.Namespace,
		}},
	}
	if err := ctrl.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.RoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, found)
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new RoleBinding", "RoleBinding.Namespace", desired.Namespace, "RoleBinding.Name", desired.Name)
		return r.Create(ctx, desired)
	}
	// RoleRef is immutable once created, and the Subjects this controller
	// sets never change for a given Dashboard name/namespace, so there's
	// nothing to reconcile drift on beyond creation.
	return err
}

// policyRulesEqual compares two PolicyRule slices ignoring order within
// each rule's string slices, since dashboardRules is a fixed literal but
// the stored object's rule order survives round-trips unpredictably only if
// the API server ever normalizes it — comparing sorted copies is cheap
// insurance either way.
func policyRulesEqual(a, b []rbacv1.PolicyRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !stringSlicesEqualSorted(a[i].APIGroups, b[i].APIGroups) ||
			!stringSlicesEqualSorted(a[i].Resources, b[i].Resources) ||
			!stringSlicesEqualSorted(a[i].Verbs, b[i].Verbs) ||
			!stringSlicesEqualSorted(a[i].ResourceNames, b[i].ResourceNames) {
			return false
		}
	}
	return true
}

// clusterRBACNameMaxLength is the Kubernetes object name limit ClusterRole/
// ClusterRoleBinding validate against (a DNS-1123 subdomain: up to 253
// characters total, with no per-label cap when — as here — the name
// contains no dots).
const clusterRBACNameMaxLength = 253

// clusterRBACName is the name of the ClusterRole and ClusterRoleBinding for
// instance's cluster metrics access. ClusterRole/ClusterRoleBinding are
// cluster-scoped, so the name must be unique across namespaces — hence the
// namespace prefix, unlike the namespace-scoped Role/RoleBinding which can
// just reuse instance.Name.
//
// The namespace is length-prefixed rather than just hyphen-joined: both
// namespace and name are valid DNS-1123 labels that may themselves contain
// hyphens, so a bare "kubepage-<namespace>-<name>" join is ambiguous (e.g.
// namespace "a", name "b-c" produces the same string as namespace "a-b",
// name "c"). Prefixing the namespace with its own length makes the encoding
// unambiguous: a decoder reads the length, then knows exactly how many
// characters of namespace follow before the separator and name.
//
// Namespace names are capped at 63 characters, but Dashboard names (like
// most Kubernetes object names) may be up to 253 — long enough that the
// encoding above can itself exceed clusterRBACNameMaxLength. When it does,
// the name is truncated and a short hash of the untruncated (namespace,
// name) pair is appended, so two different long Dashboards that happen to
// truncate to the same prefix still get distinct names.
func clusterRBACName(instance *pagev1alpha1.Dashboard) string {
	full := fmt.Sprintf("kubepage-%d-%s-%s", len(instance.Namespace), instance.Namespace, instance.Name)
	if len(full) <= clusterRBACNameMaxLength {
		return full
	}

	sum := sha256.Sum256([]byte(instance.Namespace + "/" + instance.Name))
	suffix := fmt.Sprintf("-%x", sum[:8])
	return full[:clusterRBACNameMaxLength-len(suffix)] + suffix
}

// reconcileClusterMetricsRBAC ensures the cluster-scoped RBAC for a
// kubemetrics InfoWidget exists only while one is bound to instance: it's
// created on demand and removed again when the last kubemetrics widget goes
// away, keeping the dashboard pod least-privileged (it otherwise has only
// namespace-scoped access, see dashboardRules). These objects carry no owner
// reference — a namespaced Dashboard can't own cluster-scoped objects — so
// cleanup on Dashboard deletion runs from the finalizer (deleteClusterMetricsRBAC).
func (r *DashboardReconciler) reconcileClusterMetricsRBAC(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	wanted, err := r.instanceHasKubeMetricsWidget(ctx, instance)
	if err != nil {
		return err
	}
	if !wanted {
		return r.deleteClusterMetricsRBAC(ctx, instance)
	}
	if err := r.reconcileClusterRole(ctx, instance); err != nil {
		return fmt.Errorf("reconciling ClusterRole: %w", err)
	}
	if err := r.reconcileClusterRoleBinding(ctx, instance); err != nil {
		return fmt.Errorf("reconciling ClusterRoleBinding: %w", err)
	}
	return nil
}

// instanceHasKubeMetricsWidget reports whether any InfoWidget of type
// kubemetrics is bound to instance.
func (r *DashboardReconciler) instanceHasKubeMetricsWidget(ctx context.Context, instance *pagev1alpha1.Dashboard) (bool, error) {
	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return false, fmt.Errorf("listing InfoWidgets: %w", err)
	}
	for _, w := range infoWidgets.Items {
		if w.Spec.DashboardRef.Name != instance.Name {
			continue
		}
		for _, entry := range w.Spec.Entries() {
			if entry.Type == kubeMetricsWidgetType {
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *DashboardReconciler) reconcileClusterRole(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	desired := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRBACName(instance)},
		Rules:      clusterMetricsRules,
	}

	found := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name}, found)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating a new ClusterRole", "ClusterRole.Name", desired.Name)
		return r.Create(ctx, desired)
	case err != nil:
		return err
	case !policyRulesEqual(found.Rules, desired.Rules):
		found.Rules = desired.Rules
		return r.Update(ctx, found)
	}
	return nil
}

func (r *DashboardReconciler) reconcileClusterRoleBinding(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	log := logf.FromContext(ctx)

	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterRBACName(instance)},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRBACName(instance),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      instance.Name,
			Namespace: instance.Namespace,
		}},
	}

	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name}, found)
	if apierrors.IsNotFound(err) {
		log.Info("Creating a new ClusterRoleBinding", "ClusterRoleBinding.Name", desired.Name)
		return r.Create(ctx, desired)
	}
	// RoleRef is immutable and the Subject never changes for a given
	// Dashboard name/namespace, so there's nothing to reconcile beyond creation.
	return err
}

// deleteClusterMetricsRBAC removes the cluster-scoped RBAC for instance,
// tolerating already-absent objects. Used both when the last kubemetrics
// widget is unbound and from the Dashboard finalizer on deletion. Most
// reconciles of a Dashboard with no kubemetrics widget reach here with
// nothing to clean up, so it Gets the ClusterRoleBinding first rather than
// unconditionally issuing two Deletes every time: the ClusterRole/
// ClusterRoleBinding pair is always created and deleted together by this
// file, so a missing ClusterRoleBinding means there's nothing to delete.
func (r *DashboardReconciler) deleteClusterMetricsRBAC(ctx context.Context, instance *pagev1alpha1.Dashboard) error {
	name := clusterRBACName(instance)

	crb := &rbacv1.ClusterRoleBinding{}
	switch err := r.Get(ctx, types.NamespacedName{Name: name}, crb); {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("getting ClusterRoleBinding: %w", err)
	}

	if err := r.Delete(ctx, crb); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting ClusterRoleBinding: %w", err)
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := r.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting ClusterRole: %w", err)
	}
	return nil
}

func stringSlicesEqualSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

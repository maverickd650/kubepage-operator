package controller

import (
	"context"
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

	resourcePods = "pods"
)

// clusterMetricsRules are the cluster-scoped permissions the dashboard pod
// needs when it renders a kubemetrics InfoWidget: read nodes (for capacity)
// and node metrics from metrics-server (for live usage). Granted via a
// ClusterRole rather than the per-Instance Role because both resources are
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
// Instance's own namespace to the config CRDs it renders (internal/dashboard's
// LoadSite and Poller, both cache-backed Lists). Namespace-scoped rather than
// reusing the manager's own cluster-wide ClusterRole: the dashboard pod only
// ever needs its own namespace, and granting it the manager's full
// cluster-wide permissions would be a privilege escalation for no functional
// benefit.
var dashboardConfigRule = rbacv1.PolicyRule{
	APIGroups: []string{pagev1alpha1.GroupVersion.Group},
	Resources: []string{"configurations", "serviceentries", "bookmarks", "infowidgets"},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardPodsRule is the read access the dashboard pod needs in its own
// namespace to evaluate a ServiceEntry's PodSelector
// (internal/dashboard/poller.go's monitor): listing/watching pods to compute
// "M/N ready" status. Unlike the kubemetrics ClusterRole below, this is
// namespace-scoped and owner-referenced like the rest of the per-Instance
// Role, so it's granted unconditionally rather than added/removed as
// PodSelector usage comes and goes — there's no GC/finalizer cost to it.
var dashboardPodsRule = rbacv1.PolicyRule{
	APIGroups: []string{""},
	Resources: []string{resourcePods},
	Verbs:     []string{verbGet, verbList, verbWatch},
}

// dashboardRoles builds the per-Instance Role's rules: always read access to
// the config CRDs (dashboardConfigRule) and to Pods (dashboardPodsRule),
// plus get access to exactly the Secrets referenced by the Instance's
// widgets (secretNames, already sorted and de-duplicated). The Secret rule
// is scoped with resourceNames and limited to get: the Poller only ever Gets
// a referenced Secret (internal/dashboard/poller.go resolveSecret, via a
// separate uncached client), and RBAC resourceNames can't restrict
// list/watch — so the dashboard pod can read only the credentials it
// actually uses, not every Secret in the namespace. When nothing references
// a Secret the rule is omitted entirely, since an empty resourceNames would
// instead mean "all Secrets" — the opposite of the intent.
func dashboardRoles(secretNames []string) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{dashboardConfigRule, dashboardPodsRule}
	if len(secretNames) > 0 {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			Verbs:         []string{verbGet},
			ResourceNames: secretNames,
		})
	}
	return rules
}

// referencedSecretNames returns the sorted, de-duplicated set of Secret names
// that ServiceEntries and InfoWidgets bound to instance reference via a
// secretKeyRef. It's what scopes the dashboard pod's Secret access (see
// dashboardRoles); the InstanceReconciler already re-reconciles on
// ServiceEntry/InfoWidget changes (SetupWithManager Watches), so the Role's
// scoped list stays in sync as widgets add or drop credential refs.
func (r *InstanceReconciler) referencedSecretNames(ctx context.Context, instance *pagev1alpha1.Instance) ([]string, error) {
	names := map[string]struct{}{}

	var entries pagev1alpha1.ServiceEntryList
	if err := r.List(ctx, &entries, client.InNamespace(instance.Namespace)); err != nil {
		return nil, fmt.Errorf("listing ServiceEntries: %w", err)
	}
	for _, e := range entries.Items {
		if e.Spec.InstanceRef.Name != instance.Name {
			continue
		}
		for _, w := range e.Spec.Widgets {
			for _, src := range w.Secrets {
				if src.SecretKeyRef != nil {
					names[src.SecretKeyRef.Name] = struct{}{}
				}
			}
		}
	}

	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return nil, fmt.Errorf("listing InfoWidgets: %w", err)
	}
	for _, w := range infoWidgets.Items {
		if w.Spec.InstanceRef.Name != instance.Name {
			continue
		}
		for _, src := range w.Spec.Secrets {
			if src.SecretKeyRef != nil {
				names[src.SecretKeyRef.Name] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	slices.Sort(out)
	return out, nil
}

// reconcileDashboardRBAC ensures the per-Instance ServiceAccount, Role, and
// RoleBinding the dashboard pod runs as. All three are named after instance
// and owned by it, so they're garbage-collected along with everything else
// when the Instance is deleted.
func (r *InstanceReconciler) reconcileDashboardRBAC(ctx context.Context, instance *pagev1alpha1.Instance) error {
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

func (r *InstanceReconciler) reconcileServiceAccount(ctx context.Context, instance *pagev1alpha1.Instance) error {
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

func (r *InstanceReconciler) reconcileRole(ctx context.Context, instance *pagev1alpha1.Instance) error {
	log := logf.FromContext(ctx)

	secretNames, err := r.referencedSecretNames(ctx, instance)
	if err != nil {
		return err
	}

	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Rules:      dashboardRoles(secretNames),
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

func (r *InstanceReconciler) reconcileRoleBinding(ctx context.Context, instance *pagev1alpha1.Instance) error {
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
	// sets never change for a given Instance name/namespace, so there's
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
func clusterRBACName(instance *pagev1alpha1.Instance) string {
	return fmt.Sprintf("kubepage-%d-%s-%s", len(instance.Namespace), instance.Namespace, instance.Name)
}

// reconcileClusterMetricsRBAC ensures the cluster-scoped RBAC for a
// kubemetrics InfoWidget exists only while one is bound to instance: it's
// created on demand and removed again when the last kubemetrics widget goes
// away, keeping the dashboard pod least-privileged (it otherwise has only
// namespace-scoped access, see dashboardRules). These objects carry no owner
// reference — a namespaced Instance can't own cluster-scoped objects — so
// cleanup on Instance deletion runs from the finalizer (deleteClusterMetricsRBAC).
func (r *InstanceReconciler) reconcileClusterMetricsRBAC(ctx context.Context, instance *pagev1alpha1.Instance) error {
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
func (r *InstanceReconciler) instanceHasKubeMetricsWidget(ctx context.Context, instance *pagev1alpha1.Instance) (bool, error) {
	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := r.List(ctx, &infoWidgets, client.InNamespace(instance.Namespace)); err != nil {
		return false, fmt.Errorf("listing InfoWidgets: %w", err)
	}
	for _, w := range infoWidgets.Items {
		if w.Spec.InstanceRef.Name == instance.Name && w.Spec.Type == kubeMetricsWidgetType {
			return true, nil
		}
	}
	return false, nil
}

func (r *InstanceReconciler) reconcileClusterRole(ctx context.Context, instance *pagev1alpha1.Instance) error {
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

func (r *InstanceReconciler) reconcileClusterRoleBinding(ctx context.Context, instance *pagev1alpha1.Instance) error {
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
	// Instance name/namespace, so there's nothing to reconcile beyond creation.
	return err
}

// deleteClusterMetricsRBAC removes the cluster-scoped RBAC for instance,
// tolerating already-absent objects. Used both when the last kubemetrics
// widget is unbound and from the Instance finalizer on deletion. Most
// reconciles of an Instance with no kubemetrics widget reach here with
// nothing to clean up, so it Gets the ClusterRoleBinding first rather than
// unconditionally issuing two Deletes every time: the ClusterRole/
// ClusterRoleBinding pair is always created and deleted together by this
// file, so a missing ClusterRoleBinding means there's nothing to delete.
func (r *InstanceReconciler) deleteClusterMetricsRBAC(ctx context.Context, instance *pagev1alpha1.Instance) error {
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

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
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// dashboardRules are the permissions the dashboard pod needs in its
// Instance's own namespace: read access to the config CRDs it renders
// (internal/dashboard's LoadSite and Poller, both cache-backed Lists) and to
// Secrets (Poller.resolveSecret, deliberately via a separate uncached
// client — see internal/dashboard/poller.go — but it still needs the RBAC
// regardless of which client object performs the Get). Namespace-scoped
// rather than reusing the manager's own cluster-wide ClusterRole: the
// dashboard pod only ever needs its own namespace, and granting it the
// manager's full cluster-wide permissions would be a privilege escalation
// for no functional benefit.
var dashboardRules = []rbacv1.PolicyRule{
	{
		APIGroups: []string{pagev1alpha1.GroupVersion.Group},
		Resources: []string{"configurations", "serviceentries", "bookmarks", "infowidgets"},
		Verbs:     []string{"get", "list", "watch"},
	},
	{
		APIGroups: []string{""},
		Resources: []string{"secrets"},
		Verbs:     []string{"get", "list", "watch"},
	},
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

	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Rules:      dashboardRules,
	}
	if err := ctrl.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	found := &rbacv1.Role{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, found)
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
			!stringSlicesEqualSorted(a[i].Verbs, b[i].Verbs) {
			return false
		}
	}
	return true
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

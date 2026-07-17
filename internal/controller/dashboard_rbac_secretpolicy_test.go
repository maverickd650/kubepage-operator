package controller

import (
	"context"
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	secretPolicyTestNamespace = "secretpolicy-ns"
	testAuthSecretRefName     = "htpasswd"
	testLabeledSecretName     = "labeled-secret"
	testUnlabeledSecretName   = "unlabeled-secret"
)

func newSecretPolicyTestDashboard(policy *string) *pagev1alpha1.Dashboard {
	return &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardObjName, Namespace: secretPolicyTestNamespace},
		Spec:       pagev1alpha1.DashboardSpec{ContainerPort: 8080, SecretPolicy: policy},
	}
}

func newSecretRefServiceCard(instance *pagev1alpha1.Dashboard, secretName string) *pagev1alpha1.ServiceCard {
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceCardObjName, Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Group:        "g",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testServiceCardObjName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: "prometheus",
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						secretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
							Key:                  secretField,
						}},
					},
				}},
			}},
		},
	}
}

func TestReferencedSecretNamesUnrestrictedIncludesEveryReferencedSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(nil)
	entry := newSecretRefServiceCard(instance, testUnlabeledSecretName)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testUnlabeledSecretName {
		t.Errorf("referencedSecretNames() = %v, want [unlabeled-secret] under the default Unrestricted policy", got)
	}
}

func TestReferencedSecretNamesLabeledExcludesUnlabeledSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceCard(instance, testUnlabeledSecretName)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testUnlabeledSecretName, Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("referencedSecretNames() = %v, want none: secret isn't labeled page.kubepage.dev/allow-widgets", got)
	}
}

func TestReferencedSecretNamesLabeledIncludesLabeledSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceCard(instance, testLabeledSecretName)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testLabeledSecretName, Namespace: instance.Namespace,
			Labels: map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: testValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testLabeledSecretName {
		t.Errorf("referencedSecretNames() = %v, want [labeled-secret]", got)
	}
}

func newSecretRefWidgetServiceCard(instance *pagev1alpha1.Dashboard, secretName string) *pagev1alpha1.ServiceCard {
	return &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceCardObjName, Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: instance.Name},
			Group:        "g",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testServiceCardObjName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      "prometheus",
					SecretRef: &secretName,
				}},
			}},
		},
	}
}

func TestReferencedSecretNamesUnrestrictedIncludesSecretRefSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(nil)
	entry := newSecretRefWidgetServiceCard(instance, testUnlabeledSecretName)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testUnlabeledSecretName {
		t.Errorf("referencedSecretNames() = %v, want [unlabeled-secret]: a widget's secretRef should be included the same as secretKeyRef", got)
	}
}

func TestReferencedSecretNamesLabeledExcludesUnlabeledSecretRefSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefWidgetServiceCard(instance, testUnlabeledSecretName)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testUnlabeledSecretName, Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("referencedSecretNames() = %v, want none: secretRef's Secret isn't labeled page.kubepage.dev/allow-widgets", got)
	}
}

func TestReferencedSecretNamesLabeledIncludesLabeledSecretRefSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefWidgetServiceCard(instance, testLabeledSecretName)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testLabeledSecretName, Namespace: instance.Namespace,
			Labels: map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: testValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testLabeledSecretName {
		t.Errorf("referencedSecretNames() = %v, want [labeled-secret]", got)
	}
}

func TestReferencedSecretNamesLabeledSkipsMissingSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceCard(instance, "does-not-exist")

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v, want nil: a missing Secret should be silently dropped, not error the whole reconcile", err)
	}
	if len(got) != 0 {
		t.Errorf("referencedSecretNames() = %v, want none for a nonexistent Secret", got)
	}
}

func newWidgetDefaultsTestDashboard(policy *string) *pagev1alpha1.Dashboard {
	instance := newSecretPolicyTestDashboard(policy)
	instance.Spec.WidgetDefaults = map[string]pagev1alpha1.WidgetDefaultsEntry{
		testWidgetTypePlex: {Secrets: map[string]pagev1alpha1.SecretValueSource{
			secretField: {SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testWidgetDefaultsSecretName},
				Key:                  secretField,
			}},
		}},
	}
	return instance
}

// TestReferencedSecretNamesIncludesWidgetDefaultsSecret proves the dashboard
// pod's Role includes a Secret referenced only via spec.widgetDefaults, not
// by any bound ServiceCard/InfoWidget's own secrets — the gap #109 closes:
// widgetDefaults resolves under the same RBAC as a widget's own secretKeyRef.
func TestReferencedSecretNamesIncludesWidgetDefaultsSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newWidgetDefaultsTestDashboard(nil)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testWidgetDefaultsSecretName {
		t.Errorf("referencedSecretNames() = %v, want [widget-default-secret]", got)
	}
}

// TestReferencedSecretNamesLabeledExcludesUnlabeledWidgetDefaultsSecret
// proves spec.secretPolicy: Labeled applies to a widgetDefaults-referenced
// Secret identically to a widget's own direct secretKeyRef.
func TestReferencedSecretNamesLabeledExcludesUnlabeledWidgetDefaultsSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newWidgetDefaultsTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testWidgetDefaultsSecretName, Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("referencedSecretNames() = %v, want none: widgetDefaults secret isn't labeled page.kubepage.dev/allow-widgets", got)
	}
}

// TestReferencedSecretNamesLabeledIncludesLabeledWidgetDefaultsSecret is the
// positive counterpart: a labeled widgetDefaults Secret is granted under
// spec.secretPolicy: Labeled the same as a labeled direct widget secret.
func TestReferencedSecretNamesLabeledIncludesLabeledWidgetDefaultsSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newWidgetDefaultsTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testWidgetDefaultsSecretName, Namespace: instance.Namespace,
			Labels: map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: testValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != testWidgetDefaultsSecretName {
		t.Errorf("referencedSecretNames() = %v, want [widget-default-secret]", got)
	}
}

// TestReconcileRoleIncludesWidgetDefaultsSecret exercises the full
// reconcileRole path (rather than just referencedSecretNames) to prove a
// Secret referenced only via spec.widgetDefaults actually lands in the
// reconciled Role's resourceNames.
func TestReconcileRoleIncludesWidgetDefaultsSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newWidgetDefaultsTestDashboard(nil)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	if err := r.reconcileRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileRole() error = %v", err)
	}

	var role rbacv1.Role
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &role); err != nil {
		t.Fatalf("getting reconciled Role: %v", err)
	}
	found := false
	for _, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == resourceSecrets {
			for _, name := range rule.ResourceNames {
				if name == testWidgetDefaultsSecretName {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("reconciled Role rules = %+v, want a secrets rule granting get on %q", role.Rules, testWidgetDefaultsSecretName)
	}
}

func TestAuthSecretNamesUnsetAuth(t *testing.T) {
	instance := newSecretPolicyTestDashboard(nil)
	if got := authSecretNames(instance); got != nil {
		t.Errorf("authSecretNames() = %v, want nil when spec.auth is unset", got)
	}
}

func TestAuthSecretNamesSet(t *testing.T) {
	instance := newSecretPolicyTestDashboard(nil)
	instance.Spec.Auth = &pagev1alpha1.AuthSpec{BasicAuthSecretRef: &corev1.LocalObjectReference{Name: testAuthSecretRefName}}
	got := authSecretNames(instance)
	if len(got) != 1 || got[0] != testAuthSecretRefName {
		t.Errorf("authSecretNames() = %v, want [%s]", got, testAuthSecretRefName)
	}
}

// TestReconcileRoleIncludesAuthSecretRegardlessOfSecretPolicy verifies the
// basic-auth Secret is granted RBAC even under spec.secretPolicy: Labeled
// and even when unlabeled — it's a Dashboard-spec-supplied credential, not a
// widget-supplied one, so it isn't subject to that gate (see
// authSecretNames' doc comment).
func TestReconcileRoleIncludesAuthSecretRegardlessOfSecretPolicy(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	instance.Spec.Auth = &pagev1alpha1.AuthSpec{BasicAuthSecretRef: &corev1.LocalObjectReference{Name: testAuthSecretRefName}}
	unlabeledSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testAuthSecretRefName, Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, unlabeledSecret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	if err := r.reconcileRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileRole() error = %v", err)
	}

	var role rbacv1.Role
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &role); err != nil {
		t.Fatalf("getting reconciled Role: %v", err)
	}
	found := false
	for _, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == resourceSecrets {
			for _, name := range rule.ResourceNames {
				if name == testAuthSecretRefName {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("reconciled Role rules = %+v, want a secrets rule granting get on %q", role.Rules, testAuthSecretRefName)
	}
}

// TestReconcileRoleDropsGrantWhenAllowWidgetsLabelRemoved covers the fix for
// the Secret watch in SetupWithManager: under spec.secretPolicy: Labeled, a
// Secret carrying page.kubepage.dev/allow-widgets: "true" grants its Role
// entry, and removing that label (with no change to the Dashboard itself)
// must drop the grant on the next reconcile — proving reconcileRole's
// output tracks the Secret's current label state rather than anything
// cached from when the Role was first created. In the running manager, the
// metadata-only Secret watch is what triggers this reconcile promptly;
// here we call reconcileRole directly to prove the RBAC computation itself
// is correct once triggered.
func TestReconcileRoleDropsGrantWhenAllowWidgetsLabelRemoved(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceCard(instance, testLabeledSecretName)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testLabeledSecretName, Namespace: instance.Namespace,
			Labels: map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: testValueTrue},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &DashboardReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	roleHasSecretGrant := func() bool {
		var role rbacv1.Role
		if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &role); err != nil {
			t.Fatalf("getting reconciled Role: %v", err)
		}
		for _, rule := range role.Rules {
			if len(rule.Resources) == 1 && rule.Resources[0] == resourceSecrets {
				if slices.Contains(rule.ResourceNames, testLabeledSecretName) {
					return true
				}
			}
		}
		return false
	}

	if err := r.reconcileRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileRole() error = %v", err)
	}
	if !roleHasSecretGrant() {
		t.Fatalf("Role does not grant labeled-secret while it still carries the allow-widgets label")
	}

	var current corev1.Secret
	if err := cl.Get(t.Context(), types.NamespacedName{Name: testLabeledSecretName, Namespace: instance.Namespace}, &current); err != nil {
		t.Fatalf("getting Secret: %v", err)
	}
	delete(current.Labels, pagev1alpha1.SecretAllowWidgetsLabel)
	if err := cl.Update(t.Context(), &current); err != nil {
		t.Fatalf("removing allow-widgets label: %v", err)
	}

	if err := r.reconcileRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileRole() error = %v", err)
	}
	if roleHasSecretGrant() {
		t.Errorf("Role still grants labeled-secret after its allow-widgets label was removed, want the grant dropped")
	}
}

// TestReferencedSecretNamesLabeledGetError verifies a Secret Get failure
// that isn't IsNotFound (e.g. a transient API error) propagates as an error
// from filterLabeledSecrets, rather than being treated the same as a
// missing Secret.
func TestReferencedSecretNamesLabeledGetError(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestDashboard(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceCard(instance, "some-secret")
	wantErr := errors.New("get Secret boom")

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	directReader := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
			if _, ok := o.(*corev1.Secret); ok {
				return wantErr
			}
			return c.Get(ctx, key, o, opts...)
		},
	})
	r := &DashboardReconciler{Client: base, Scheme: scheme, DirectReader: directReader}

	if _, err := r.referencedSecretNames(t.Context(), instance); !errors.Is(err, wantErr) {
		t.Errorf("referencedSecretNames() error = %v, want wrapping %v", err, wantErr)
	}
}

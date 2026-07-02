package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const secretPolicyTestNamespace = "secretpolicy-ns"

func newSecretPolicyTestInstance(policy *string) *pagev1alpha1.Instance {
	return &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceObjName, Namespace: secretPolicyTestNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080, SecretPolicy: policy},
	}
}

func newSecretRefServiceEntry(instance *pagev1alpha1.Instance, secretName string) *pagev1alpha1.ServiceEntry {
	return &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: instance.Namespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: instance.Name},
			Group:       "g",
			Name:        "svc",
			Widgets: []pagev1alpha1.ServiceWidget{{
				Type: "prometheus",
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					"token": {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "token",
					}},
				},
			}},
		},
	}
}

func TestReferencedSecretNamesUnrestrictedIncludesEveryReferencedSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestInstance(nil)
	entry := newSecretRefServiceEntry(instance, "unlabeled-secret")

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != "unlabeled-secret" {
		t.Errorf("referencedSecretNames() = %v, want [unlabeled-secret] under the default Unrestricted policy", got)
	}
}

func TestReferencedSecretNamesLabeledExcludesUnlabeledSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestInstance(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceEntry(instance, "unlabeled-secret")
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unlabeled-secret", Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

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
	instance := newSecretPolicyTestInstance(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceEntry(instance, "labeled-secret")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "labeled-secret", Namespace: instance.Namespace,
			Labels: map[string]string{pagev1alpha1.SecretAllowWidgetsLabel: "true"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry, secret).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v", err)
	}
	if len(got) != 1 || got[0] != "labeled-secret" {
		t.Errorf("referencedSecretNames() = %v, want [labeled-secret]", got)
	}
}

func TestReferencedSecretNamesLabeledSkipsMissingSecret(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestInstance(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	entry := newSecretRefServiceEntry(instance, "does-not-exist")

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, entry).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	got, err := r.referencedSecretNames(t.Context(), instance)
	if err != nil {
		t.Fatalf("referencedSecretNames() error = %v, want nil: a missing Secret should be silently dropped, not error the whole reconcile", err)
	}
	if len(got) != 0 {
		t.Errorf("referencedSecretNames() = %v, want none for a nonexistent Secret", got)
	}
}

func TestAuthSecretNamesUnsetAuth(t *testing.T) {
	instance := newSecretPolicyTestInstance(nil)
	if got := authSecretNames(instance); got != nil {
		t.Errorf("authSecretNames() = %v, want nil when spec.auth is unset", got)
	}
}

func TestAuthSecretNamesSet(t *testing.T) {
	instance := newSecretPolicyTestInstance(nil)
	instance.Spec.Auth = &pagev1alpha1.AuthSpec{BasicAuthSecretRef: &corev1.LocalObjectReference{Name: "htpasswd"}}
	got := authSecretNames(instance)
	if len(got) != 1 || got[0] != "htpasswd" {
		t.Errorf("authSecretNames() = %v, want [htpasswd]", got)
	}
}

// TestReconcileRoleIncludesAuthSecretRegardlessOfSecretPolicy verifies the
// basic-auth Secret is granted RBAC even under spec.secretPolicy: Labeled
// and even when unlabeled — it's an Instance-spec-supplied credential, not a
// widget-supplied one, so it isn't subject to that gate (see
// authSecretNames' doc comment).
func TestReconcileRoleIncludesAuthSecretRegardlessOfSecretPolicy(t *testing.T) {
	scheme := networkTestScheme(t)
	instance := newSecretPolicyTestInstance(ptr.To(pagev1alpha1.SecretPolicyLabeled))
	instance.Spec.Auth = &pagev1alpha1.AuthSpec{BasicAuthSecretRef: &corev1.LocalObjectReference{Name: "htpasswd"}}
	unlabeledSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "htpasswd", Namespace: instance.Namespace}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, unlabeledSecret).Build()
	r := &InstanceReconciler{Client: cl, Scheme: scheme, DirectReader: cl}

	if err := r.reconcileRole(t.Context(), instance); err != nil {
		t.Fatalf("reconcileRole() error = %v", err)
	}

	var role rbacv1.Role
	if err := cl.Get(t.Context(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, &role); err != nil {
		t.Fatalf("getting reconciled Role: %v", err)
	}
	found := false
	for _, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == "secrets" {
			for _, name := range rule.ResourceNames {
				if name == "htpasswd" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("reconciled Role rules = %+v, want a secrets rule granting get on %q", role.Rules, "htpasswd")
	}
}

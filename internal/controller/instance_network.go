package controller

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// serviceForInstance returns the ClusterIP Service fronting instance's
// homepage pods, owned by instance.
func (r *InstanceReconciler) serviceForInstance(instance *pagev1alpha1.Instance) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labelsForInstance(),
			Ports: []corev1.ServicePort{{
				Name:       instanceContainerName,
				Port:       instance.Spec.ContainerPort,
				TargetPort: intstr.FromInt32(instance.Spec.ContainerPort),
			}},
		},
	}
	if err := ctrl.SetControllerReference(instance, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

// reconcileService ensures the homepage Service for instance exists and
// matches the desired state. Every Instance gets a Service (it's how its
// Deployment is reached at all, Ingress or not), so unlike Ingress there's
// no enabled/disabled toggle here.
func (r *InstanceReconciler) reconcileService(ctx context.Context, instance *pagev1alpha1.Instance) error {
	log := logf.FromContext(ctx)

	desired, err := r.serviceForInstance(instance)
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
	if !portsEqual(found.Spec.Ports, desired.Spec.Ports) || !maps.Equal(found.Spec.Selector, desired.Spec.Selector) {
		found.Spec.Ports = desired.Spec.Ports
		found.Spec.Selector = desired.Spec.Selector
		found.Spec.ClusterIP = clusterIP // immutable once set; preserve it
		if err := r.Update(ctx, found); err != nil {
			return fmt.Errorf("updating Service %s/%s: %w", found.Namespace, found.Name, err)
		}
	}
	return nil
}

// ingressForInstance returns the Ingress exposing instance's Service at
// spec.ingress.host, owned by instance. Only called when spec.ingress is set
// and enabled.
func (r *InstanceReconciler) ingressForInstance(instance *pagev1alpha1.Instance) (*networkingv1.Ingress, error) {
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
func (r *InstanceReconciler) reconcileIngress(ctx context.Context, instance *pagev1alpha1.Instance) error {
	log := logf.FromContext(ctx)

	enabled := instance.Spec.Ingress != nil && instance.Spec.Ingress.Enabled

	found := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	switch {
	case apierrors.IsNotFound(err):
		if !enabled {
			return nil
		}
		desired, err := r.ingressForInstance(instance)
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

	desired, err := r.ingressForInstance(instance)
	if err != nil {
		return fmt.Errorf("defining Ingress: %w", err)
	}
	if !ingressSpecsEqual(found.Spec, desired.Spec) || !maps.Equal(found.Annotations, desired.Annotations) {
		found.Spec = desired.Spec
		found.Annotations = desired.Annotations
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

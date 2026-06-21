package render

// Kubernetes renders kubernetes.yaml with the given mode ("disabled",
// "default", or "cluster" — see homepage's kubernetes.yaml docs). Per the
// operator's CRD-only discovery posture (D5), every Instance gets mode
// "disabled" unless an InfoWidget of type "kubernetes" is bound, in which
// case the Instance controller switches this to "cluster" so that widget can
// fetch the cluster stats it displays.
func Kubernetes(mode string) ([]byte, error) {
	return ToYAML(map[string]any{"mode": mode})
}

// KubernetesDisabled renders kubernetes.yaml with discovery explicitly
// turned off — the default for every Instance with no "kubernetes"
// InfoWidget bound.
func KubernetesDisabled() ([]byte, error) {
	return Kubernetes("disabled")
}

package render

// KubernetesDisabled renders kubernetes.yaml with discovery explicitly
// turned off. Per the operator's CRD-only discovery posture (see
// IMPLEMENTATION_PLAN.md decision D5), every Instance gets this file unless
// an InfoWidget of type "kubernetes" asks for the cluster connection
// (Phase 4), so homepage never auto-discovers services the operator doesn't
// already know about.
func KubernetesDisabled() ([]byte, error) {
	return ToYAML(map[string]any{"mode": "disabled"})
}

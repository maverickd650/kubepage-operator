package dashboard

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func kubeMetricsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := metricsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func node(name, allocCPU, allocMem string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(allocCPU),
				corev1.ResourceMemory: resource.MustParse(allocMem),
			},
		},
	}
}

func nodeMetrics(name, usageCPU, usageMem string) *metricsv1beta1.NodeMetrics {
	return &metricsv1beta1.NodeMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Usage: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(usageCPU),
			corev1.ResourceMemory: resource.MustParse(usageMem),
		},
	}
}

// TestKubeMetricsWidgetPollNeverCalledInProduction documents Poll's stub
// contract: kubemetrics is a ClusterWidget, so the poller always prefers
// PollCluster and never reaches this — but it still has to satisfy the
// Widget interface to live in the registry (see widget.go's ClusterWidget
// doc comment), so a regression that makes Poll panic instead of returning
// its documented error would only ever surface if that invariant broke.
func TestKubeMetricsWidgetPollNeverCalledInProduction(t *testing.T) {
	fields, err := (kubeMetricsWidget{}).Poll(t.Context(), nil, WidgetConfig{})
	if fields != nil {
		t.Errorf("Poll() fields = %+v, want nil", fields)
	}
	if err == nil {
		t.Fatal("Poll() error = nil, want a cluster-only error")
	}
}

func TestKubeMetricsWidgetSample(t *testing.T) {
	tests := map[string]struct {
		config       string
		wantCPULabel string
		wantMemLabel string
	}{
		"default labels": {wantCPULabel: labelCPU, wantMemLabel: labelMemory},
		"custom labels": {
			config:       `{"cpuLabel":"Compute","memoryLabel":"RAM"}`,
			wantCPULabel: "Compute",
			wantMemLabel: "RAM",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := (kubeMetricsWidget{}).Sample(WidgetConfig{Config: []byte(tc.config)})
			if len(got) != 2 || got[0].Label != tc.wantCPULabel || got[1].Label != tc.wantMemLabel {
				t.Fatalf("Sample() = %+v, want labels %q/%q", got, tc.wantCPULabel, tc.wantMemLabel)
			}
			if got[0].Percent == nil || got[1].Percent == nil {
				t.Error("Sample() fields have no Percent, want usage bars in a preview")
			}
		})
	}
}

func TestUsageHighlight(t *testing.T) {
	tests := map[string]struct {
		pct  *int
		want string
	}{
		"nil":      {pct: nil, want: ""},
		"danger":   {pct: new(90), want: HighlightDanger},
		"warn":     {pct: new(75), want: HighlightWarn},
		"below 75": {pct: new(74), want: ""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := usageHighlight(tc.pct); got != tc.want {
				t.Errorf("usageHighlight(%v) = %q, want %q", tc.pct, got, tc.want)
			}
		})
	}
}

func TestKubeMetricsWidgetPollCluster(t *testing.T) {
	scheme := kubeMetricsScheme(t)

	tests := map[string]struct {
		objs   []client.Object
		config string
		want   []Field
	}{
		"usage with capacity": {
			objs: []client.Object{
				nodeMetrics("n1", "500m", "2Gi"),
				nodeMetrics("n2", "1500m", "2Gi"),
				node("n1", "4", "8Gi"),
				node("n2", "4", "8Gi"),
			},
			want: []Field{
				{Label: labelCPU, Value: "2 / 8 cores (25%)", Percent: new(25)},
				{Label: labelMemory, Value: "4 / 16 GiB (25%)", Percent: new(25)},
			},
		},
		"custom labels": {
			objs: []client.Object{
				nodeMetrics("n1", "1", "1Gi"),
				node("n1", "2", "4Gi"),
			},
			config: `{"cpuLabel":"Compute","memoryLabel":"RAM"}`,
			want: []Field{
				{Label: "Compute", Value: "1 / 2 cores (50%)", Percent: new(50)},
				{Label: "RAM", Value: "1 / 4 GiB (25%)", Percent: new(25)},
			},
		},
		"no capacity omits percentage": {
			objs: []client.Object{
				nodeMetrics("n1", "1500m", "3Gi"),
			},
			want: []Field{
				{Label: labelCPU, Value: "1.5 cores"},
				{Label: labelMemory, Value: "3 GiB"},
			},
		},
		"high usage sets highlight": {
			objs: []client.Object{
				nodeMetrics("n1", "900m", "900Mi"),
				node("n1", "1", "1000Mi"),
			},
			want: []Field{
				{Label: labelCPU, Value: "0.9 / 1 cores (90%)", Percent: new(90), Highlight: HighlightDanger},
				{Label: labelMemory, Value: "0.9 / 1 GiB (90%)", Percent: new(90), Highlight: HighlightDanger},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).Build()
			got, err := (kubeMetricsWidget{}).PollCluster(t.Context(), c, WidgetConfig{
				Config: []byte(tc.config),
			})
			if err != nil {
				t.Fatalf("PollCluster() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("PollCluster() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

package v1alpha1

import "testing"

const (
	testDashboardHome  = "home"
	testDashboardOther = "other"
)

// TestRefName verifies RefName's nil handling: a nil dashboardRef (unset)
// returns "", a non-nil one returns its Name.
func TestRefName(t *testing.T) {
	if got := RefName(nil); got != "" {
		t.Errorf("RefName(nil) = %q, want \"\"", got)
	}
	if got := RefName(&DashboardRef{Name: testDashboardHome}); got != testDashboardHome {
		t.Errorf("RefName(&DashboardRef{Name: %q}) = %q, want %q", testDashboardHome, got, testDashboardHome)
	}
}

// TestBoundTo covers every combination BoundTo's callers rely on: an
// explicit ref binds only to the Dashboard it names regardless of how many
// Dashboards exist in the namespace, and an unset ref binds only when the
// namespace has exactly one Dashboard.
func TestBoundTo(t *testing.T) {
	tests := []struct {
		name                    string
		refName                 string
		dashboardName           string
		namespaceDashboardCount int
		want                    bool
	}{
		{"explicit ref matching", testDashboardHome, testDashboardHome, 1, true},
		{"explicit ref matching, multiple Dashboards in namespace", testDashboardHome, testDashboardHome, 2, true},
		{"explicit ref not matching", testDashboardOther, testDashboardHome, 1, false},
		{"unset ref, sole Dashboard", "", testDashboardHome, 1, true},
		{"unset ref, no Dashboard in namespace", "", testDashboardHome, 0, false},
		{"unset ref, multiple Dashboards in namespace", "", testDashboardHome, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoundTo(tt.refName, tt.dashboardName, tt.namespaceDashboardCount); got != tt.want {
				t.Errorf("BoundTo(%q, %q, %d) = %v, want %v", tt.refName, tt.dashboardName, tt.namespaceDashboardCount, got, tt.want)
			}
		})
	}
}

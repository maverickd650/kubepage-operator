package controller

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// labelValueRegexp mirrors Kubernetes' label value validation:
// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
var labelValueRegexp = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

func TestImageVersionLabel(t *testing.T) {
	tests := map[string]struct {
		image string
		want  string
	}{
		"tag reference": {
			image: "example.com/kubepage-operator:v1.2.3",
			want:  "v1.2.3",
		},
		"tag reference with registry port": {
			image: "example.com:5000/kubepage-operator:v1.2.3",
			want:  "v1.2.3",
		},
		"untagged reference": {
			image: "example.com/kubepage-operator",
			want:  "",
		},
		"digest reference": {
			image: "example.com/kubepage-operator@sha256:725beb947b49ab1c6f25a6aeabc2a7288e5a58e341477ee1eb2b54fa37178c7",
			want:  "725beb947b49ab1c6f25a6aeabc2a7288e5a58e341477ee1eb2b54fa37178c7",
		},
		"digest reference with registry port": {
			image: "example.com:5000/kubepage-operator@sha256:725beb947b49ab1c6f25a6aeabc2a7288e5a58e341477ee1eb2b54fa37178c7",
			want:  "725beb947b49ab1c6f25a6aeabc2a7288e5a58e341477ee1eb2b54fa37178c7",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := imageVersionLabel(tc.image)
			if got != tc.want {
				t.Errorf("imageVersionLabel(%q) = %q, want %q", tc.image, got, tc.want)
			}
			if len(got) > maxLabelValueLen {
				t.Errorf("imageVersionLabel(%q) = %q, len %d exceeds Kubernetes label value limit of %d", tc.image, got, len(got), maxLabelValueLen)
			}
		})
	}
}

func TestImageVersionLabelTruncatesOverlongDigest(t *testing.T) {
	// A synthetic digest longer than any real sha256 hex digest, to prove
	// truncation kicks in defensively regardless of digest algorithm.
	longDigest := strings.Repeat("a", 100)
	image := "example.com/kubepage-operator@sha512:" + longDigest

	got := imageVersionLabel(image)
	if len(got) != maxLabelValueLen {
		t.Fatalf("imageVersionLabel() len = %d, want exactly %d (truncated)", len(got), maxLabelValueLen)
	}
	if got != longDigest[:maxLabelValueLen] {
		t.Errorf("imageVersionLabel() = %q, want the first %d bytes of the digest", got, maxLabelValueLen)
	}
}

func TestLabelsForDashboardIncludesSelectorLabelsPlusVersion(t *testing.T) {
	labels := labelsForDashboard("my-dashboard", "example.com/kubepage-operator@sha256:725beb947b49ab1c6f25a6aeabc2a7288e5a58e341477ee1eb2b54fa37178c7")

	for k, v := range selectorLabelsForDashboard("my-dashboard") {
		if labels[k] != v {
			t.Errorf("labelsForDashboard()[%q] = %q, want %q (from selectorLabelsForDashboard)", k, labels[k], v)
		}
	}
	version := labels["app.kubernetes.io/version"]
	if len(version) > maxLabelValueLen {
		t.Errorf("labelsForDashboard() version label len = %d, exceeds Kubernetes limit of %d", len(version), maxLabelValueLen)
	}
}

func TestSelectorLabelsForDashboardDistinguishesInstances(t *testing.T) {
	a := selectorLabelsForDashboard("dashboard-a")
	b := selectorLabelsForDashboard("dashboard-b")

	if a["app.kubernetes.io/instance"] == b["app.kubernetes.io/instance"] {
		t.Fatalf("selectorLabelsForDashboard() produced the same instance label for different Dashboard names: %q", a["app.kubernetes.io/instance"])
	}
	if reflect.DeepEqual(a, b) {
		t.Errorf("selectorLabelsForDashboard(%q) and selectorLabelsForDashboard(%q) should differ, got identical label sets %+v", "dashboard-a", "dashboard-b", a)
	}
}

func TestInstanceLabelValueTruncatesOverlongName(t *testing.T) {
	// Two Dashboard names sharing a 63-char prefix should still map to
	// distinct label values (via the appended hash), and each result must
	// stay within the Kubernetes label value length limit.
	prefix := strings.Repeat("a", maxLabelValueLen)
	nameA := prefix + "-one"
	nameB := prefix + "-two"

	valA := instanceLabelValue(nameA)
	valB := instanceLabelValue(nameB)

	if len(valA) != maxLabelValueLen {
		t.Fatalf("instanceLabelValue(%q) len = %d, want exactly %d", nameA, len(valA), maxLabelValueLen)
	}
	if len(valB) != maxLabelValueLen {
		t.Fatalf("instanceLabelValue(%q) len = %d, want exactly %d", nameB, len(valB), maxLabelValueLen)
	}
	if valA == valB {
		t.Errorf("instanceLabelValue() produced identical values for two different long names sharing a prefix: %q", valA)
	}
	if !labelValueRegexp.MatchString(valA) {
		t.Errorf("instanceLabelValue(%q) = %q is not a valid Kubernetes label value", nameA, valA)
	}
	if !labelValueRegexp.MatchString(valB) {
		t.Errorf("instanceLabelValue(%q) = %q is not a valid Kubernetes label value", nameB, valB)
	}
}

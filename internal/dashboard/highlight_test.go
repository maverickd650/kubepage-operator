package dashboard

import (
	"testing"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	testFieldQueued = "queued"
	testFieldStatus = "status"
)

func TestFilterFields(t *testing.T) {
	fields := []Field{{Label: testFieldQueued, Value: "1"}, {Label: "wanted", Value: "2"}, {Label: testFieldStatus, Value: "ok"}}

	if got := filterFields(fields, nil); len(got) != len(fields) {
		t.Errorf("filterFields(nil allowlist) = %+v, want unchanged", got)
	}

	got := filterFields(fields, []string{testFieldStatus, testFieldQueued})
	want := []Field{{Label: testFieldQueued, Value: "1"}, {Label: testFieldStatus, Value: "ok"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("filterFields(...) = %+v, want %+v (original order preserved)", got, want)
	}
}

func TestApplyHighlightsFirstMatchWins(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		testFieldQueued: {Rules: []pagev1alpha1.HighlightRuleSpec{
			{Level: "danger", When: whenGTE, Value: "20"},
			{Level: "warn", When: whenGTE, Value: "5"},
			{Level: "good", When: whenEQ, Value: "0"},
		}},
	}

	cases := []struct {
		value string
		want  string
	}{
		{"25", HighlightDanger},
		{"10", HighlightWarn},
		{"0", HighlightGood},
		{"2", ""},
	}
	for _, tc := range cases {
		fields := []Field{{Label: testFieldQueued, Value: tc.value}}
		got := applyHighlights(fields, rules)
		if got[0].Highlight != tc.want {
			t.Errorf("applyHighlights(value=%q) = %q, want %q", tc.value, got[0].Highlight, tc.want)
		}
	}
}

func TestApplyHighlightsDoesNotOverrideWidgetSet(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		"cpu": {Rules: []pagev1alpha1.HighlightRuleSpec{{Level: "danger", When: whenGTE, Value: "0"}}},
	}
	fields := []Field{{Label: "cpu", Value: "90", Highlight: HighlightWarn}}
	got := applyHighlights(fields, rules)
	if got[0].Highlight != HighlightWarn {
		t.Errorf("applyHighlights overrode a widget-set Highlight: got %q, want unchanged %q", got[0].Highlight, HighlightWarn)
	}
}

func TestEvaluateNumericRules(t *testing.T) {
	value2 := func(s string) *string { return &s }

	cases := []struct {
		name  string
		rule  pagev1alpha1.HighlightRuleSpec
		value string
		want  bool
	}{
		{"gt match", pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: "5"}, "10", true},
		{"gt no match", pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: "5"}, "5", false},
		{"lte boundary", pagev1alpha1.HighlightRuleSpec{When: whenLTE, Value: "5"}, "5", true},
		{"ne match", pagev1alpha1.HighlightRuleSpec{When: whenNE, Value: "5"}, "6", true},
		{"between inside", pagev1alpha1.HighlightRuleSpec{When: whenBetween, Value: "10", Value2: value2("20")}, "15", true},
		{"between outside bound", pagev1alpha1.HighlightRuleSpec{When: whenBetween, Value: "10", Value2: value2("20")}, "25", false},
		{"outside match", pagev1alpha1.HighlightRuleSpec{When: whenOutside, Value: "10", Value2: value2("20")}, "25", true},
		{"tolerates formatted value", pagev1alpha1.HighlightRuleSpec{When: whenGTE, Value: "20"}, "45%", true},
		{"unparseable value", pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: "5"}, "n/a", false},
		{"negated", pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: "5", Negate: new(true)}, "10", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleMatches(tc.rule, tc.value); got != tc.want {
				t.Errorf("ruleMatches(%+v, %q) = %v, want %v", tc.rule, tc.value, got, tc.want)
			}
		})
	}
}

func TestEvaluateStringRules(t *testing.T) {
	cases := []struct {
		name  string
		rule  pagev1alpha1.HighlightRuleSpec
		value string
		want  bool
	}{
		{"equals case-insensitive by default", pagev1alpha1.HighlightRuleSpec{When: whenEquals, Value: "OK"}, "ok", true},
		{"equals case-sensitive", pagev1alpha1.HighlightRuleSpec{When: whenEquals, Value: "OK", CaseSensitive: new(true)}, "ok", false},
		{"includes", pagev1alpha1.HighlightRuleSpec{When: whenIncludes, Value: "pending"}, "import pending", true},
		{"startsWith", pagev1alpha1.HighlightRuleSpec{When: whenStartsWith, Value: "5"}, "503", true},
		{"endsWith", pagev1alpha1.HighlightRuleSpec{When: whenEndsWith, Value: "02"}, "503 502", true},
		{"regex", pagev1alpha1.HighlightRuleSpec{When: whenRegex, Value: `^5\d{2}$`}, "503", true},
		{"regex case-insensitive by default", pagev1alpha1.HighlightRuleSpec{When: whenRegex, Value: "failed"}, "FAILED", true},
		{"regex invalid pattern", pagev1alpha1.HighlightRuleSpec{When: whenRegex, Value: "("}, "anything", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleMatches(tc.rule, tc.value); got != tc.want {
				t.Errorf("ruleMatches(%+v, %q) = %v, want %v", tc.rule, tc.value, got, tc.want)
			}
		})
	}
}

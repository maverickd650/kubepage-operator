package dashboard

import (
	"strings"
	"testing"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	testFieldQueued = "queued"
	testFieldStatus = "status"
	testNotANumber  = "n/a"
	testBogusWhen   = "bogus"
	// testKeySceneSize is a homepage-vocabulary field key (camelCase) whose
	// normalized form matches this package's "Scene Size" label.
	testKeySceneSize = "sceneSize"
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

// TestFilterFieldsNormalizedMatch verifies allowlist matching is
// case/punctuation-insensitive, so a homepage-vocabulary `fields` entry
// (camelCase or snake_case) matches this package's human-readable
// Field.Label without dropping every field silently.
func TestFilterFieldsNormalizedMatch(t *testing.T) {
	fields := []Field{
		{Label: labelScenes, Value: "1"},
		{Label: labelSceneSize, Value: "2"},
		{Label: labelOCount, Value: "3"},
		{Label: labelImages, Value: "4"},
	}

	tests := map[string]struct {
		allowlist []string
		want      []Field
	}{
		"camelCase homepage keys": {
			allowlist: []string{testKeySceneSize, "oCount"},
			want:      []Field{{Label: labelSceneSize, Value: "2"}, {Label: labelOCount, Value: "3"}},
		},
		"snake_case keys": {
			allowlist: []string{"scene_size"},
			want:      []Field{{Label: labelSceneSize, Value: "2"}},
		},
		"exact-case label still matches": {
			allowlist: []string{labelScenes},
			want:      []Field{{Label: labelScenes, Value: "1"}},
		},
		"different case entirely": {
			allowlist: []string{"IMAGES"},
			want:      []Field{{Label: labelImages, Value: "4"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := filterFields(fields, tc.allowlist)
			if len(got) != len(tc.want) {
				t.Fatalf("filterFields(%v) = %+v, want %+v", tc.allowlist, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("filterFields(%v)[%d] = %+v, want %+v", tc.allowlist, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeFieldKey(t *testing.T) {
	const wantSceneSize = "scenesize"
	tests := map[string]string{
		"Scene Size":     wantSceneSize,
		testKeySceneSize: wantSceneSize,
		"scene_size":     wantSceneSize,
		"O Count":        "ocount",
		"oCount":         "ocount",
		"":               "",
		"ABC-123_def":    "abc123def",
	}
	for in, want := range tests {
		if got := normalizeFieldKey(in); got != want {
			t.Errorf("normalizeFieldKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestApplyHighlightsFirstMatchWins(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		testFieldQueued: {Rules: []pagev1alpha1.HighlightRuleSpec{
			{Level: HighlightDanger, When: whenGTE, Value: "20"},
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

// TestApplyHighlightsNormalizedKey is the regression guard for the
// highlight/fields vocabulary split: a highlight rule keyed with the same
// homepage-style vocabulary that filterFields accepts (e.g. "sceneSize") must
// fire against this package's human-readable label ("Scene Size"), matched the
// same normalized way filterFields matches its allowlist.
func TestApplyHighlightsNormalizedKey(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		testKeySceneSize: {Rules: []pagev1alpha1.HighlightRuleSpec{
			{Level: HighlightDanger, When: whenGTE, Value: "100"},
		}},
	}
	fields := []Field{{Label: "Scene Size", Value: "120"}}
	got := applyHighlights(fields, rules)
	if got[0].Highlight != HighlightDanger {
		t.Errorf("applyHighlights() = %q, want %q for homepage-vocabulary rule key matching normalized label", got[0].Highlight, HighlightDanger)
	}
}

func TestApplyHighlightsDoesNotOverrideWidgetSet(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		testCPUName: {Rules: []pagev1alpha1.HighlightRuleSpec{{Level: HighlightDanger, When: whenGTE, Value: "0"}}},
	}
	fields := []Field{{Label: testCPUName, Value: "90", Highlight: HighlightWarn}}
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
		{"unparseable value", pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: "5"}, testNotANumber, false},
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

func TestApplyHighlightsFieldNotInRules(t *testing.T) {
	rules := map[string]pagev1alpha1.FieldHighlight{
		testFieldStatus: {Rules: []pagev1alpha1.HighlightRuleSpec{{Level: HighlightDanger, When: whenEquals, Value: "down"}}},
	}
	fields := []Field{{Label: testFieldQueued, Value: "5"}}
	got := applyHighlights(fields, rules)
	if got[0].Highlight != "" {
		t.Errorf("applyHighlights() = %q, want empty for a field with no matching rule entry", got[0].Highlight)
	}
}

func TestNumericValueOverflowIsUnparseable(t *testing.T) {
	huge := strings.Repeat("9", 400)
	if _, ok := numericValue(huge); ok {
		t.Errorf("numericValue(%d nines) = ok, want unparseable (float64 overflow)", len(huge))
	}
}

func TestEvaluateNumericRuleUnparseableBound(t *testing.T) {
	r := pagev1alpha1.HighlightRuleSpec{When: whenGT, Value: testNotANumber}
	if evaluateNumericRule(r, "10") {
		t.Errorf("evaluateNumericRule() = true, want false for an unparseable rule bound")
	}
}

func TestEvaluateNumericRuleLessThan(t *testing.T) {
	r := pagev1alpha1.HighlightRuleSpec{When: whenLT, Value: "10"}
	if !evaluateNumericRule(r, "5") {
		t.Errorf("evaluateNumericRule(lt) = false, want true for 5 < 10")
	}
	if evaluateNumericRule(r, "10") {
		t.Errorf("evaluateNumericRule(lt) = true, want false for 10 < 10")
	}
}

func TestEvaluateNumericRuleBetweenMissingValue2(t *testing.T) {
	r := pagev1alpha1.HighlightRuleSpec{When: whenBetween, Value: "10"}
	if evaluateNumericRule(r, "15") {
		t.Errorf("evaluateNumericRule(between) = true, want false when Value2 is nil")
	}
}

func TestEvaluateNumericRuleBetweenUnparseableValue2(t *testing.T) {
	bad := testNotANumber
	r := pagev1alpha1.HighlightRuleSpec{When: whenBetween, Value: "10", Value2: &bad}
	if evaluateNumericRule(r, "15") {
		t.Errorf("evaluateNumericRule(between) = true, want false for an unparseable Value2")
	}
}

func TestEvaluateNumericRuleBetweenReversedBounds(t *testing.T) {
	lo := "20"
	r := pagev1alpha1.HighlightRuleSpec{When: whenBetween, Value: "10", Value2: &lo}
	// Value (10) > Value2 (20) is backwards from the documented lo/hi order;
	// the implementation should still treat it as the [10,20] range.
	if !evaluateNumericRule(r, "15") {
		t.Errorf("evaluateNumericRule(between, reversed bounds) = false, want true for 15 within [10,20]")
	}
}

func TestEvaluateNumericRuleUnknownOperator(t *testing.T) {
	// evaluateRule only ever dispatches the documented operators into
	// evaluateNumericRule; call it directly to exercise its defensive default.
	r := pagev1alpha1.HighlightRuleSpec{When: testBogusWhen, Value: "5"}
	if evaluateNumericRule(r, "10") {
		t.Errorf("evaluateNumericRule(unknown When) = true, want false")
	}
}

func TestEvaluateStringRuleUnknownOperator(t *testing.T) {
	r := pagev1alpha1.HighlightRuleSpec{When: testBogusWhen, Value: "x"}
	if evaluateStringRule(r, "x") {
		t.Errorf("evaluateStringRule(unknown When) = true, want false")
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

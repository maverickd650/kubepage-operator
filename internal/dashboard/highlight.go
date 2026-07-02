package dashboard

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// HighlightRuleSpec.When's operator values (api/v1alpha1/serviceentry_types.go's
// kubebuilder enum). Named here rather than left as inline literals since
// each is referenced from more than one of the functions below.
const (
	whenGT      = "gt"
	whenGTE     = "gte"
	whenLT      = "lt"
	whenLTE     = "lte"
	whenEQ      = "eq"
	whenNE      = "ne"
	whenBetween = "between"
	whenOutside = "outside"

	whenEquals     = "equals"
	whenIncludes   = "includes"
	whenStartsWith = "startsWith"
	whenEndsWith   = "endsWith"
	whenRegex      = "regex"
)

// filterFields restricts fields to those whose Label appears in allowlist,
// preserving the widget's original field order. An empty allowlist (the
// default: a ServiceWidget with no Fields set) returns fields unchanged.
func filterFields(fields []Field, allowlist []string) []Field {
	if len(allowlist) == 0 {
		return fields
	}
	keep := make(map[string]bool, len(allowlist))
	for _, label := range allowlist {
		keep[label] = true
	}
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if keep[f.Label] {
			out = append(out, f)
		}
	}
	return out
}

// applyHighlights evaluates rules (keyed by Field.Label, from a
// ServiceWidget's Highlight map) against fields, setting Field.Highlight on
// the first matching rule per field. A field that already carries a
// widget-set Highlight (e.g. kubemetrics' own CPU/memory thresholds) is left
// untouched: this generic engine only fills in what the widget itself didn't
// already decide.
func applyHighlights(fields []Field, rules map[string]pagev1alpha1.FieldHighlight) []Field {
	if len(rules) == 0 {
		return fields
	}
	for i := range fields {
		f := &fields[i]
		if f.Highlight != "" {
			continue
		}
		fh, ok := rules[f.Label]
		if !ok {
			continue
		}
		if level := firstMatchingLevel(fh.Rules, f.Value); level != "" {
			f.Highlight = level
		}
	}
	return fields
}

// firstMatchingLevel returns the Level of the first rule in rules that
// matches value, evaluated in declaration order (homepage's documented
// behavior), or "" if none match.
func firstMatchingLevel(rules []pagev1alpha1.HighlightRuleSpec, value string) string {
	for _, r := range rules {
		if ruleMatches(r, value) {
			return r.Level
		}
	}
	return ""
}

func ruleMatches(r pagev1alpha1.HighlightRuleSpec, value string) bool {
	matched := evaluateRule(r, value)
	if r.Negate != nil && *r.Negate == pagev1alpha1.NegateNegate {
		return !matched
	}
	return matched
}

func evaluateRule(r pagev1alpha1.HighlightRuleSpec, value string) bool {
	switch r.When {
	case whenGT, whenGTE, whenLT, whenLTE, whenEQ, whenNE, whenBetween, whenOutside:
		return evaluateNumericRule(r, value)
	default:
		return evaluateStringRule(r, value)
	}
}

// numberPattern extracts the first decimal number found in a field's
// formatted value, tolerating surrounding text (e.g. "12 ms", "45%",
// "1.2 GiB") — homepage's own highlight engine documents the same
// best-effort coercion, recommending plain numbers for reliable results.
var numberPattern = regexp.MustCompile(`-?\d+(\.\d+)?`)

func numericValue(s string) (float64, bool) {
	m := numberPattern.FindString(s)
	if m == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func evaluateNumericRule(r pagev1alpha1.HighlightRuleSpec, value string) bool {
	v, ok := numericValue(value)
	if !ok {
		return false
	}
	bound, ok := numericValue(r.Value)
	if !ok {
		return false
	}

	switch r.When {
	case whenGT:
		return v > bound
	case whenGTE:
		return v >= bound
	case whenLT:
		return v < bound
	case whenLTE:
		return v <= bound
	case whenEQ:
		return v == bound
	case whenNE:
		return v != bound
	case whenBetween, whenOutside:
		if r.Value2 == nil {
			return false
		}
		bound2, ok := numericValue(*r.Value2)
		if !ok {
			return false
		}
		lo, hi := bound, bound2
		if lo > hi {
			lo, hi = hi, lo
		}
		within := v >= lo && v <= hi
		if r.When == whenOutside {
			return !within
		}
		return within
	default:
		return false
	}
}

func evaluateStringRule(r pagev1alpha1.HighlightRuleSpec, value string) bool {
	if r.When == whenRegex {
		return evaluateRegexRule(r, value)
	}

	caseSensitive := r.CaseSensitive != nil && *r.CaseSensitive == pagev1alpha1.CaseSensitiveOn
	v, target := value, r.Value
	if !caseSensitive {
		v, target = strings.ToLower(v), strings.ToLower(target)
	}

	switch r.When {
	case whenEquals:
		return v == target
	case whenIncludes:
		return strings.Contains(v, target)
	case whenStartsWith:
		return strings.HasPrefix(v, target)
	case whenEndsWith:
		return strings.HasSuffix(v, target)
	default:
		return false
	}
}

// compiledRegex caches a regexp.Compile result (success or failure) so a
// repeated invalid pattern doesn't pay the compile cost on every poll either.
type compiledRegex struct {
	re  *regexp.Regexp
	err error
}

// regexCache caches evaluateRegexRule's compiled patterns across polls,
// keyed by the literal pattern string (including the "(?i)" prefix
// evaluateRegexRule may add): the same HighlightRuleSpec is evaluated on
// every poll cycle for as long as its ServiceCard/InfoWidget exists, and
// regexp.Compile was otherwise re-parsing the identical pattern every time.
var regexCache sync.Map // pattern string -> compiledRegex

func compileRegexCached(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		c := v.(compiledRegex) //nolint:forcetypeassert // regexCache only ever stores compiledRegex
		return c.re, c.err
	}
	re, err := regexp.Compile(pattern)
	regexCache.Store(pattern, compiledRegex{re: re, err: err})
	return re, err
}

func evaluateRegexRule(r pagev1alpha1.HighlightRuleSpec, value string) bool {
	pattern := r.Value
	if r.CaseSensitive == nil || *r.CaseSensitive != pagev1alpha1.CaseSensitiveOn {
		pattern = "(?i)" + pattern
	}
	re, err := compileRegexCached(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

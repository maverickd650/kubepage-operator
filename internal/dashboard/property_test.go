package dashboard

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

var validHexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func TestPropertyAccentHexAlwaysValidHex(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		color := rapid.String().Draw(t, "color")
		hex := AccentHex(color)
		if !validHexRe.MatchString(hex) {
			t.Fatalf("AccentHex(%q) = %q, not a valid #RRGGBB hex", color, hex)
		}
	})
}

func TestPropertyPaletteRampAlwaysHas10Shades(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		color := rapid.String().Draw(t, "color")
		ramp := PaletteRamp(color)
		if ramp.C50 == "" || ramp.C100 == "" || ramp.C200 == "" || ramp.C300 == "" ||
			ramp.C400 == "" || ramp.C500 == "" || ramp.C600 == "" || ramp.C700 == "" ||
			ramp.C800 == "" || ramp.C900 == "" {
			t.Fatalf("PaletteRamp(%q) has an empty shade: %+v", color, ramp)
		}
	})
}

func TestPropertyPercentBarStyleClamps(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := rapid.Int().Draw(t, "p")
		result := percentBarStyle(p)
		if !strings.HasPrefix(result, "width:") || !strings.HasSuffix(result, "%;") {
			t.Fatalf("percentBarStyle(%d) = %q, want \"width: N%%;\" format", p, result)
		}
	})
}

// hasUnescapedQuote reports whether s contains a `"` not preceded by an odd
// number of backslashes — i.e. one that would actually terminate a CSS
// string literal rather than being read as an escaped quote.
// cssStringEscape escapes backslashes before quotes (see render_helpers.go),
// so every quote it produces from an original `"` is preceded by an odd
// backslash count; this only flags a quote that slipped through unescaped.
func hasUnescapedQuote(s string) bool {
	backslashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			backslashes++
			continue
		case '"':
			if backslashes%2 == 0 {
				return true
			}
		}
		backslashes = 0
	}
	return false
}

// TestPropertyCSSStringEscapeNeutralizesInjection generates adversarial
// strings biased toward the exact characters cssStringEscape must neutralize
// (quotes, angle brackets, backslashes), since pure random text rarely
// produces these by chance. The invariant is not "no literal quote
// character" (a backslash-escaped quote is safe and expected) but "no quote
// that would terminate a CSS string literal", and "no literal angle
// bracket" (those are fully replaced by HTML entities, so their absence is
// the correct check).
func TestPropertyCSSStringEscapeNeutralizesInjection(t *testing.T) {
	adversarial := rapid.StringOfN(rapid.SampledFrom([]rune{'"', '<', '>', '\\', 'a', ' '}), 0, 40, -1)
	rapid.Check(t, func(t *rapid.T) {
		s := adversarial.Draw(t, "s")
		escaped := cssStringEscape(s)
		if strings.Contains(escaped, "<") || strings.Contains(escaped, ">") {
			t.Fatalf("cssStringEscape(%q) = %q, still contains an unescaped angle bracket", s, escaped)
		}
		if hasUnescapedQuote(escaped) {
			t.Fatalf("cssStringEscape(%q) = %q, contains a quote that would terminate the CSS string", s, escaped)
		}
	})
}

// TestPropertyJSStringEscapeNoScriptClose is biased toward "</script"-shaped
// substrings (in varying case) since that's the exact pattern jsStringEscape
// must break.
func TestPropertyJSStringEscapeNoScriptClose(t *testing.T) {
	fragment := rapid.SampledFrom([]string{"</script", "</SCRIPT", "</Script>", "x", " ", "script"})
	adversarial := rapid.Map(rapid.SliceOfN(fragment, 0, 5), func(parts []string) string {
		return strings.Join(parts, "")
	})
	rapid.Check(t, func(t *rapid.T) {
		s := adversarial.Draw(t, "s")
		escaped := jsStringEscape(s)
		if strings.Contains(strings.ToLower(escaped), "</script") {
			t.Fatalf("jsStringEscape(%q) = %q, still contains an unescaped </script", s, escaped)
		}
	})
}

// TestPropertyCSSBlockEscapeNoStyleClose mirrors
// TestPropertyJSStringEscapeNoScriptClose above, but for cssBlockEscape
// (used by customStyle to embed CustomCSS as raw <style> text content): it's
// biased toward "</style"-shaped substrings in varying case, since that's
// the exact pattern cssBlockEscape must break.
func TestPropertyCSSBlockEscapeNoStyleClose(t *testing.T) {
	fragment := rapid.SampledFrom([]string{"</style", "</STYLE", "</Style>", "x", " ", "style"})
	adversarial := rapid.Map(rapid.SliceOfN(fragment, 0, 5), func(parts []string) string {
		return strings.Join(parts, "")
	})
	rapid.Check(t, func(t *rapid.T) {
		s := adversarial.Draw(t, "s")
		escaped := cssBlockEscape(s)
		if strings.Contains(strings.ToLower(escaped), "</style") {
			t.Fatalf("cssBlockEscape(%q) = %q, still contains an unescaped </style", s, escaped)
		}
	})
}

func TestPropertyNumericValueNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "s")
		numericValue(s) // must not panic regardless of input
	})
}

// TestPropertyFilterFieldsPreservesOrder draws allowlists from the same
// small label vocabulary as the fixed field set below (plus an
// intentionally-absent label) so allowlists actually intersect the fields
// being filtered, rather than almost always matching everything or nothing.
func TestPropertyFilterFieldsPreservesOrder(t *testing.T) {
	fields := []Field{
		{Label: "A", Value: "1"},
		{Label: "B", Value: "2"},
		{Label: "C", Value: "3"},
		{Label: "D", Value: "4"},
	}
	label := rapid.SampledFrom([]string{"A", "B", "C", "D", "X"})
	allowlist := rapid.SliceOfN(label, 0, 6)

	rapid.Check(t, func(t *rapid.T) {
		allow := allowlist.Draw(t, "allow")
		result := filterFields(fields, allow)

		if len(allow) > 0 && len(result) > len(fields) {
			t.Fatalf("filterFields(%v, %v) returned %d fields, more than the %d input fields",
				fields, allow, len(result), len(fields))
		}

		lastIdx := -1
		for _, r := range result {
			idx := -1
			for i, orig := range fields {
				if r.Label == orig.Label {
					idx = i
					break
				}
			}
			if idx <= lastIdx {
				t.Fatalf("filterFields(%v, %v) = %v, did not preserve input order", fields, allow, result)
			}
			lastIdx = idx
		}
	})
}

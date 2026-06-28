package dashboard

import "testing"

func TestCompareOrder(t *testing.T) {
	one := int32(1)
	two := int32(2)
	tests := map[string]struct {
		a, b *int32
		want int
	}{
		"both nil":         {a: nil, b: nil, want: 0},
		"a nil sorts last": {a: nil, b: &one, want: 1},
		"b nil sorts last": {a: &one, b: nil, want: -1},
		"a less than b":    {a: &one, b: &two, want: -1},
		"a greater than b": {a: &two, b: &one, want: 1},
		"equal":            {a: &one, b: &one, want: 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := compareOrder(tc.a, tc.b); got != tc.want {
				t.Errorf("compareOrder(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

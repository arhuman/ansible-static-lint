package rules

import "testing"

// TestInvalidPermission pins the arithmetic behind risky-octal: an octal mode a
// person would actually write is plausible, and the same digits read as decimal
// are not, which is exactly the mistake the rule exists to catch.
func TestInvalidPermission(t *testing.T) {
	plausible := []int64{0o777, 0o755, 0o750, 0o700, 0o711, 0o644, 0o640, 0o600, 0o444, 0o400}
	for _, mode := range plausible {
		if invalidPermission(mode) {
			t.Errorf("0o%o judged invalid, want valid", mode)
		}
	}
	// The same digits typed without the leading zero, which YAML reads as
	// decimal and ansible then applies as a very different mode.
	asDecimal := []int64{777, 755, 750, 700, 711, 644, 640, 600, 444, 400}
	for _, mode := range asDecimal {
		if !invalidPermission(mode) {
			t.Errorf("%d judged valid, want invalid", mode)
		}
	}
	// Group or other more generous than the class above it is implausible too.
	for _, mode := range []int64{0o466, 0o647, 0o067} {
		if !invalidPermission(mode) {
			t.Errorf("0o%o judged valid, want invalid", mode)
		}
	}
}

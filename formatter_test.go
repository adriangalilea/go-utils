package utils

import "testing"

// The twin of @adriangalilea/utils bytes(): one rule on both sides.
func TestBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{4096, "4096 B"},
		{999_999, "999999 B"},
		{1_000_000, "1.0 MB"},
		{500_000_000, "500.0 MB"},
		{850_000_000_000, "850.0 GB"},
		{2_000_000_000_000, "2.0 TB"},
		{12_700_000_000_000, "12.7 TB"},
		{3_000_000_000_000_000, "3.0 PB"},
	}
	for _, c := range cases {
		if got := Bytes(c.n); got != c.want {
			t.Errorf("Bytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
	if got := Bytes(400 * GB); got != "400.0 GB" {
		t.Errorf("a stated 400 GB prints %q", got)
	}
}

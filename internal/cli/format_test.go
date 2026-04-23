package cli

import "testing"

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		ms   float64
		want string
	}{
		{"sub-ms", 0.4, "0.4ms"},
		{"tens of ms", 23.4, "23.4ms"},
		{"just under 1s", 999.9, "999.9ms"},
		{"exactly 1s", 1000, "1.0s"},
		{"tens of seconds", 23456.7, "23.5s"},
		{"just under 100s", 99000, "99.0s"},
		{"100s flips to no-decimals", 100000, "100s"},
		{"thousands of seconds", 2925000, "2,925s"},
		{"millions", 7662000000, "7,662,000s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.ms); got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{100, "100"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{1456832, "1,456,832"},
		{-1234, "-1,234"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatCount(tt.in); got != tt.want {
				t.Errorf("formatCount(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	tests := []struct {
		name   string
		num    float64
		denom  float64
		want   string
	}{
		{"basic", 382, 1000, "38.2%"},
		{"zero denom", 10, 0, "—"},
		{"zero num", 0, 100, "0.0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPercent(tt.num, tt.denom); got != tt.want {
				t.Errorf("formatPercent(%v,%v) = %q, want %q", tt.num, tt.denom, got, tt.want)
			}
		})
	}
}

func TestFormatCacheHitPct(t *testing.T) {
	tests := []struct {
		name string
		hit  int64
		read int64
		want string
	}{
		{"all hit", 100, 0, "100.0%"},
		{"all miss", 0, 100, "0.0%"},
		{"typical", 984, 16, "98.4%"},
		{"no accesses", 0, 0, "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCacheHitPct(tt.hit, tt.read); got != tt.want {
				t.Errorf("formatCacheHitPct(%d,%d) = %q, want %q", tt.hit, tt.read, got, tt.want)
			}
		})
	}
}

func TestTruncateQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		maxWidth int
		want     string
	}{
		{"short unchanged", "SELECT 1", 20, "SELECT 1"},
		{"whitespace collapsed", "SELECT\n   *\tFROM   users", 40, "SELECT * FROM users"},
		{"leading/trailing trimmed", "   SELECT 1   ", 40, "SELECT 1"},
		{"truncated", "SELECT * FROM orders WHERE customer_id = $1", 20, "SELECT * FROM order…"},
		{"maxWidth 1 produces ellipsis", "SELECT 1", 1, "…"},
		{"unbounded", "SELECT * FROM long", 0, "SELECT * FROM long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateQuery(tt.query, tt.maxWidth)
			if got != tt.want {
				t.Errorf("truncateQuery = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollapseWhitespace(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"a  b\t\tc\nd", "a b c d"},
		{"  leading", "leading"},
		{"trailing   ", "trailing"},
	}
	for _, tt := range tests {
		got := collapseWhitespace(tt.in)
		if got != tt.want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

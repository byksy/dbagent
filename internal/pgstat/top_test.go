package pgstat

import (
	"strings"
	"testing"
)

func TestIsInPreload(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"exact", "pg_stat_statements", true},
		{"with neighbour no space", "pg_stat_statements,pg_cron", true},
		{"with neighbour leading space", "pg_cron, pg_stat_statements", true},
		{"trailing whitespace", "pg_cron, pg_stat_statements ", true},
		{"mixed case", "PG_Stat_Statements", true},
		{"unrelated", "pgaudit", false},
		{"unrelated multi", "pgaudit,timescaledb", false},
		{"substring only", "pg_stat_statements_extra", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInPreload(tt.in); got != tt.want {
				t.Errorf("isInPreload(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildTopQuery_OrderBy(t *testing.T) {
	tests := []struct {
		name    string
		orderBy string
		wantCol string
		wantErr bool
	}{
		{"total", "total", "total_exec_time", false},
		{"mean", "mean", "mean_exec_time", false},
		{"calls", "calls", "calls", false},
		{"invalid", "rows", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, err := buildTopQuery(tt.orderBy)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (sql=%q)", sql)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(sql, "ORDER BY "+tt.wantCol+" DESC") {
				t.Errorf("sql missing ORDER BY %q clause:\n%s", tt.wantCol, sql)
			}
			if !strings.Contains(sql, "LIMIT $1") {
				t.Errorf("sql missing parameterized LIMIT: %s", sql)
			}
		})
	}
}

func TestBuildTopQuery_NoUserInputInterpolation(t *testing.T) {
	// A malicious order_by must not sneak into the generated SQL.
	_, err := buildTopQuery("total_exec_time; DROP TABLE users--")
	if err == nil {
		t.Fatalf("expected rejection of non-whitelisted order_by")
	}
}

func TestIsExtensionMissingErr(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{"nil", "", false},
		{"does not exist", `relation "pg_stat_statements" does not exist`, true},
		{"not loaded", "pg_stat_statements must be loaded via shared_preload_libraries", true},
		{"unrelated", "permission denied", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.msg != "" {
				err = &fakeErr{msg: tt.msg}
			}
			if got := isExtensionMissingErr(err); got != tt.want {
				t.Errorf("got %v, want %v for %q", got, tt.want, tt.msg)
			}
		})
	}
}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

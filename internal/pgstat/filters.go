package pgstat

import "strings"

// systemQueryFilters is the SQL WHERE fragment that excludes noise
// queries from pg_stat_statements — dbagent's own probes,
// information-schema / pg_catalog scans, transaction-control
// statements, and maintenance commands (ANALYZE / VACUUM / REINDEX).
// The goal is that a default `dbagent stats` or `dbagent top` call
// shows only user workload, not the chatter of tooling and DBAs.
//
// Patterns are ILIKE so they match regardless of case. Each entry is
// a plain substring with the '%' wildcards spliced in — no regex, no
// user input — so the fragment is safe to concatenate into a query.
var systemQueryFilters = []string{
	"%pg_stat_statements%",
	"%pg_extension%",
	"%pg_catalog.%",
	"%information_schema.%",
	"SET %",
	"SHOW %",
	"BEGIN%",
	"COMMIT%",
	"ROLLBACK%",
	"ANALYZE%",
	"VACUUM%",
	"REINDEX%",
	"CHECKPOINT%",
	"CLUSTER%",
}

// systemQueryFilterSQL returns the WHERE clauses that exclude system
// queries, one per line starting with "AND". An empty string when
// includeSystem is true so callers can paste the result unconditionally
// after another WHERE clause.
func systemQueryFilterSQL(includeSystem bool) string {
	if includeSystem {
		return ""
	}
	var b strings.Builder
	for _, pat := range systemQueryFilters {
		// Pattern is an untrusted-looking literal in source, but it is
		// a compile-time constant; no user input flows in.
		b.WriteString("\n  AND query NOT ILIKE '")
		b.WriteString(strings.ReplaceAll(pat, "'", "''"))
		b.WriteString("'")
	}
	return b.String()
}

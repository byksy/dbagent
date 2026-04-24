package schema

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fetch runs a read-only transaction against pool and returns a
// populated Schema. All queries target pg_catalog directly (not
// information_schema) and exclude system schemas. No user tables are
// read, only metadata.
func Fetch(ctx context.Context, pool *pgxpool.Pool) (*Schema, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("schema: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var serverVersion, database string
	if err := tx.QueryRow(ctx, `SELECT current_setting('server_version'), current_database()`).
		Scan(&serverVersion, &database); err != nil {
		return nil, fmt.Errorf("schema: read server metadata: %w", err)
	}

	s := &Schema{
		Meta: Meta{
			ExportedAt:    time.Now().UTC(),
			Database:      database,
			ServerVersion: serverVersion,
		},
		Tables:  map[string]*Table{},
		Indexes: map[string]*Index{},
	}

	if err := fetchTables(ctx, tx, s); err != nil {
		return nil, err
	}
	if err := fetchColumns(ctx, tx, s); err != nil {
		return nil, err
	}
	if err := fetchIndexes(ctx, tx, s); err != nil {
		return nil, err
	}
	if err := fetchForeignKeys(ctx, tx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// fetchTables populates s.Tables with one entry per ordinary table.
func fetchTables(ctx context.Context, tx pgx.Tx, s *Schema) error {
	const q = `
SELECT
    n.nspname,
    c.relname,
    c.reltuples::bigint,
    pg_total_relation_size(c.oid),
    s.last_analyze,
    s.last_vacuum
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_%'
ORDER BY n.nspname, c.relname`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("schema: fetch tables: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			t                        Table
			lastAnalyzed, lastVacuum *time.Time
		)
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowEstimate, &t.SizeBytes, &lastAnalyzed, &lastVacuum); err != nil {
			return fmt.Errorf("schema: scan table: %w", err)
		}
		if lastAnalyzed != nil {
			t.LastAnalyzed = lastAnalyzed.UTC()
		}
		if lastVacuum != nil {
			t.LastVacuumed = lastVacuum.UTC()
		}
		fqn := qualify(t.Schema, t.Name)
		tCopy := t
		s.Tables[fqn] = &tCopy
	}
	return rows.Err()
}

// fetchColumns populates Columns on tables already in s.Tables. Any
// column for a table we didn't load (e.g., a non-ordinary relation
// that slipped through) is silently skipped.
func fetchColumns(ctx context.Context, tx pgx.Tx, s *Schema) error {
	const q = `
SELECT
    n.nspname,
    c.relname,
    a.attname,
    format_type(a.atttypid, a.atttypmod),
    a.attnotnull,
    COALESCE(pg_get_expr(d.adbin, d.adrelid), ''),
    a.attnum
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE c.relkind = 'r'
  AND a.attnum > 0
  AND NOT a.attisdropped
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_%'
ORDER BY n.nspname, c.relname, a.attnum`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("schema: fetch columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			schemaName, tableName string
			col                   Column
		)
		if err := rows.Scan(&schemaName, &tableName, &col.Name, &col.DataType, &col.NotNull, &col.Default, &col.Position); err != nil {
			return fmt.Errorf("schema: scan column: %w", err)
		}
		t, ok := s.Tables[qualify(schemaName, tableName)]
		if !ok {
			continue
		}
		t.Columns = append(t.Columns, col)
	}
	return rows.Err()
}

// fetchIndexes populates s.Indexes. Expression-index entries appear
// with a NULL attname in the catalog; we exclude any index that
// contains such a position entirely. Keeping only the direct column
// references would misrepresent a `(lower(email), created_at)`
// index as `(created_at)`, which would let HasIndexOn claim
// coverage the index doesn't actually provide.
func fetchIndexes(ctx context.Context, tx pgx.Tx, s *Schema) error {
	const q = `
SELECT
    n.nspname,
    i.relname,
    t.relname,
    tn.nspname,
    ix.indisunique,
    ix.indisprimary,
    ix.indpred IS NOT NULL,
    COALESCE(pg_get_expr(ix.indpred, ix.indrelid), ''),
    am.amname,
    pg_relation_size(i.oid),
    pg_get_indexdef(ix.indexrelid),
    array_agg(a.attname ORDER BY ord.n),
    COALESCE(us.idx_scan, 0)
FROM pg_index ix
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_namespace n ON n.oid = i.relnamespace
JOIN pg_namespace tn ON tn.oid = t.relnamespace
JOIN pg_am am ON am.oid = i.relam
LEFT JOIN unnest(ix.indkey) WITH ORDINALITY AS ord(attnum, n) ON true
LEFT JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = ord.attnum
LEFT JOIN pg_stat_user_indexes us ON us.indexrelid = ix.indexrelid
WHERE t.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_%'
GROUP BY n.nspname, i.relname, t.relname, tn.nspname,
         ix.indisunique, ix.indisprimary, ix.indpred, ix.indrelid,
         am.amname, i.oid, ix.indexrelid, us.idx_scan
ORDER BY n.nspname, i.relname`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("schema: fetch indexes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			idx                Index
			tableName, tableNS string
			rawCols            []*string
		)
		if err := rows.Scan(
			&idx.Schema, &idx.Name,
			&tableName, &tableNS,
			&idx.IsUnique, &idx.IsPrimary, &idx.IsPartial, &idx.WhereExpr,
			&idx.Method, &idx.SizeBytes, &idx.Definition, &rawCols, &idx.Scans,
		); err != nil {
			return fmt.Errorf("schema: scan index: %w", err)
		}
		idx.Table = qualify(tableNS, tableName)
		hasExpression := false
		for _, c := range rawCols {
			if c == nil {
				// Expression-index position. Mark and bail — we
				// cannot represent this index faithfully with our
				// column-based model, and rules that act on it
				// would mis-suggest.
				hasExpression = true
				break
			}
			idx.Columns = append(idx.Columns, *c)
		}
		if hasExpression {
			continue
		}
		s.Indexes[qualify(idx.Schema, idx.Name)] = &idx
	}
	return rows.Err()
}

// fetchForeignKeys populates s.FKeys.
func fetchForeignKeys(ctx context.Context, tx pgx.Tx, s *Schema) error {
	const q = `
SELECT
    n.nspname,
    con.conname,
    c.relname,
    fn.nspname,
    fc.relname,
    (SELECT array_agg(a.attname ORDER BY k.n)
     FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, n)
     JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum) AS columns,
    (SELECT array_agg(a.attname ORDER BY k.n)
     FROM unnest(con.confkey) WITH ORDINALITY AS k(attnum, n)
     JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.attnum) AS ref_columns
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_class fc ON fc.oid = con.confrelid
JOIN pg_namespace fn ON fn.oid = fc.relnamespace
WHERE con.contype = 'f'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_%'
ORDER BY n.nspname, c.relname, con.conname`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("schema: fetch fkeys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fk ForeignKey
		if err := rows.Scan(
			&fk.Schema, &fk.Name,
			&fk.Table,
			&fk.ReferencedSchema, &fk.ReferencedTable,
			&fk.Columns, &fk.ReferencedColumns,
		); err != nil {
			return fmt.Errorf("schema: scan fkey: %w", err)
		}
		s.FKeys = append(s.FKeys, fk)
	}
	return rows.Err()
}

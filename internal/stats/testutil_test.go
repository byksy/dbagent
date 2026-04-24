package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/byksy/dbagent/internal/pgstat"
)

// fixtureJSON is the on-disk shape used by the fixture loader.
// Keeping it file-local means the stats public API never leaks raw
// JSON shapes; tests translate once here and hand the clean types to
// the code under test.
type fixtureJSON struct {
	ServerVersion string          `json:"server_version"`
	Database      string          `json:"database"`
	StatsSince    time.Time       `json:"stats_since"`
	Queries       []fixtureQuery  `json:"queries"`
}

type fixtureQuery struct {
	QueryID           int64   `json:"queryid"`
	Query             string  `json:"query"`
	Calls             int64   `json:"calls"`
	TotalExecTime     float64 `json:"total_exec_time"`
	MeanExecTime      float64 `json:"mean_exec_time"`
	Rows              int64   `json:"rows"`
	SharedBlksHit     int64   `json:"shared_blks_hit"`
	SharedBlksRead    int64   `json:"shared_blks_read"`
	SharedBlksDirtied int64   `json:"shared_blks_dirtied"`
	SharedBlksWritten int64   `json:"shared_blks_written"`
}

// loadFixture reads a testdata/stats/<name> JSON file and returns
// the rows + meta ready to feed into ComputeFromRows.
func loadFixture(t *testing.T, name string) ([]RawQueryRow, Meta) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "stats", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var f fixtureJSON
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	rows := make([]RawQueryRow, 0, len(f.Queries))
	for _, q := range f.Queries {
		rows = append(rows, pgstat.WorkloadRow{
			QueryID:           q.QueryID,
			Query:             q.Query,
			Calls:             q.Calls,
			TotalExecTimeMs:   q.TotalExecTime,
			MeanExecTimeMs:    q.MeanExecTime,
			Rows:              q.Rows,
			SharedBlksHit:     q.SharedBlksHit,
			SharedBlksRead:    q.SharedBlksRead,
			SharedBlksDirtied: q.SharedBlksDirtied,
			SharedBlksWritten: q.SharedBlksWritten,
		})
	}
	meta := Meta{
		Database:       f.Database,
		ServerVersion:  f.ServerVersion,
		SnapshotAt:     time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
		StatsSince:     f.StatsSince,
		DBAgentVersion: "test",
	}
	return rows, meta
}

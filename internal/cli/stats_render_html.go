package cli

import (
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/byksy/dbagent/internal/cli/style"
	"github.com/byksy/dbagent/internal/rules"
	"github.com/byksy/dbagent/internal/stats"
)

// renderStatsHTML writes a single self-contained HTML report to w.
// The document embeds its CSS and uses `prefers-color-scheme` so the
// same file renders correctly on light and dark clients. It is
// designed to survive being pasted into email, attached to tickets,
// or opened from disk — no external requests at all.
func renderStatsHTML(w io.Writer, ws *stats.WorkloadStats) error {
	data := newHTMLData(ws)
	return htmlTemplate.Execute(w, data)
}

// htmlData bundles everything the template needs. Keeping it flat
// means the template stays readable and template-funcmap tricks
// stay minimal.
type htmlData struct {
	Title              string
	CSSLight, CSSDark  string
	Meta               stats.Meta
	Overview           htmlOverview
	Sections           []htmlSection
	Recommendations    []htmlRecommendation
}

type htmlOverview struct {
	TotalTimeMs      float64
	TotalExecutions  int64
	TotalQueries     int64
	ReadShare        float64
	WriteShare       float64
	OtherShare       float64
	CacheHitRatio    float64
	CacheHitSeverity string
}

type htmlSection struct {
	ID    string
	Title string
	Rows  []htmlRow
}

type htmlRow struct {
	Rank     int
	Calls    int64
	MeanMs   float64
	TotalMs  float64
	Share    float64
	Reads    int64
	Hits     int64
	CacheHit float64
	Query    string
}

type htmlRecommendation struct {
	Severity string
	Title    string
	Message  string
	Action   string
}

// newHTMLData assembles the template data from a WorkloadStats. The
// transforms live in one place so renderer behaviour stays parallel
// to the terminal output.
func newHTMLData(ws *stats.WorkloadStats) htmlData {
	d := htmlData{
		Title:    fmt.Sprintf("dbagent stats — %s", ws.Meta.Database),
		Meta:     ws.Meta,
		CSSLight: cssFromPalette(false),
		CSSDark:  cssFromPalette(true),
	}

	if ws.TotalTimeMs > 0 {
		d.Overview.ReadShare = ws.ReadTimeMs / ws.TotalTimeMs
		d.Overview.WriteShare = ws.WriteTimeMs / ws.TotalTimeMs
		other := 1 - d.Overview.ReadShare - d.Overview.WriteShare
		if other < 0 {
			other = 0
		}
		d.Overview.OtherShare = other
	}
	d.Overview.TotalTimeMs = ws.TotalTimeMs
	d.Overview.TotalExecutions = ws.TotalExecutions
	d.Overview.TotalQueries = ws.TotalQueries
	d.Overview.CacheHitRatio = ws.CacheHitRatio
	d.Overview.CacheHitSeverity = cacheHitSeverityClass(ws.CacheHitRatio)

	d.Sections = []htmlSection{
		{ID: "top-total-time", Title: "Top Queries by Total Time", Rows: rowsForTimeView(ws.TopByTotalTime)},
		{ID: "top-call-count", Title: "Top Queries by Call Count", Rows: rowsForTimeView(ws.TopByCallCount)},
		{ID: "top-io", Title: "Top Queries by I/O Reads", Rows: rowsForIOView(ws.TopByIO)},
		{ID: "top-low-cache", Title: "Top Queries by Low Cache Hit", Rows: rowsForIOView(ws.TopByLowCache)},
	}

	for _, rec := range ws.Recommendations {
		d.Recommendations = append(d.Recommendations, htmlRecommendation{
			Severity: severityClass(rec.Severity),
			Title:    rec.Title,
			Message:  rec.Message,
			Action:   rec.Action,
		})
	}
	return d
}

func rowsForTimeView(qs []stats.QueryGroup) []htmlRow {
	out := make([]htmlRow, 0, len(qs))
	for _, q := range qs {
		out = append(out, htmlRow{
			Rank:     q.Rank,
			Calls:    q.Calls,
			MeanMs:   q.MeanTimeMs,
			TotalMs:  q.TotalTimeMs,
			Share:    q.PctOfTotal,
			CacheHit: -1, // time-based sections don't expose cache data; -1 → "—"
			Query:    q.QueryText,
		})
	}
	return out
}

func rowsForIOView(qs []stats.QueryGroup) []htmlRow {
	out := make([]htmlRow, 0, len(qs))
	for _, q := range qs {
		out = append(out, htmlRow{
			Rank:     q.Rank,
			Reads:    q.SharedBlksRead,
			Hits:     q.SharedBlksHit,
			CacheHit: q.CacheHitRatio,
			TotalMs:  q.TotalTimeMs,
			Query:    q.QueryText,
		})
	}
	return out
}

// cssFromPalette renders the :root-level CSS variable block from
// the shared style.CSSVariables map. Template keeps both the light
// (default) and dark (media-query-wrapped) versions.
func cssFromPalette(dark bool) string {
	vars := style.CSSVariables(dark)
	keys := []string{
		"--color-critical", "--color-warning", "--color-info", "--color-success",
		"--color-muted", "--color-text", "--color-bg", "--color-border",
		"--color-bar-fill", "--color-bar-empty",
	}
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s;\n", k, vars[k])
	}
	return b.String()
}

// cacheHitSeverityClass maps a workload-wide cache ratio to a CSS
// class on the progress bar so the fill colour tracks severity.
func cacheHitSeverityClass(ratio float64) string {
	switch {
	case ratio < 0:
		return "muted"
	case ratio < 0.7:
		return "critical"
	case ratio < 0.9:
		return "warning"
	}
	return "ok"
}

// severityClass maps rules.Severity to a CSS class name.
func severityClass(s rules.Severity) string {
	switch s {
	case rules.SeverityCritical:
		return "critical"
	case rules.SeverityWarning:
		return "warning"
	case rules.SeverityInfo:
		return "info"
	}
	return "muted"
}

// htmlTemplate is the single master template. Inline styles keep
// the file self-contained; no CDN, no external assets. Template
// syntax deliberately stays simple — most formatting happens in Go.
var htmlTemplate = template.Must(template.New("stats").Funcs(template.FuncMap{
	"pct": func(r float64) string {
		if r < 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f%%", r*100)
	},
	"pctInt": func(r float64) string {
		if r < 0 {
			return "—"
		}
		return fmt.Sprintf("%d%%", int(r*100+0.5))
	},
	"ms": func(v float64) string {
		if v < 1000 {
			return fmt.Sprintf("%.1fms", v)
		}
		if v < 60*1000 {
			return fmt.Sprintf("%.1fs", v/1000)
		}
		m := int(v / 60000)
		s := int(v/1000) % 60
		return fmt.Sprintf("%dm %02ds", m, s)
	},
	"commas": func(n int64) string {
		neg := n < 0
		if neg {
			n = -n
		}
		s := fmt.Sprint(n)
		if len(s) <= 3 {
			if neg {
				return "-" + s
			}
			return s
		}
		first := len(s) % 3
		var out string
		if first > 0 {
			out = s[:first]
		}
		for i := first; i < len(s); i += 3 {
			if out != "" {
				out += ","
			}
			out += s[i : i+3]
		}
		if neg {
			return "-" + out
		}
		return out
	},
	"ratioToWidth": func(r float64) string {
		if r < 0 {
			r = 0
		}
		if r > 1 {
			r = 1
		}
		return fmt.Sprintf("%.1f%%", r*100)
	},
	"iso": func(t interface{}) string {
		if v, ok := t.(interface{ UTC() interface{} }); ok {
			_ = v
		}
		return fmt.Sprintf("%v", t)
	},
}).Parse(htmlTemplateSrc))

// htmlTemplateSrc keeps the HTML+CSS source in one place. Changes
// here propagate through cli and are reflected in golden tests.
const htmlTemplateSrc = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ .Title }}</title>
<style>
:root {
{{ .CSSLight }}
}
@media (prefers-color-scheme: dark) {
  :root {
{{ .CSSDark }}
  }
}
body {
  font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  margin: 0; padding: 24px;
  background: var(--color-bg);
  color: var(--color-text);
  max-width: 960px;
  margin-inline: auto;
}
h1, h2 { margin-top: 1.6em; }
h1 { font-size: 1.4em; }
h2 { font-size: 1.1em; border-bottom: 1px solid var(--color-border); padding-bottom: 4px; }
nav { margin: 16px 0; font-size: 0.9em; }
nav a { margin-right: 12px; color: var(--color-info); text-decoration: none; }
nav a:hover { text-decoration: underline; }
.meta p { margin: 2px 0; color: var(--color-muted); }
section { margin-top: 28px; }
table { border-collapse: collapse; width: 100%; font-variant-numeric: tabular-nums; }
th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--color-border); }
th { color: var(--color-muted); font-weight: 500; font-size: 0.85em; }
td.num { text-align: right; }
td.query { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.9em; }
.bar {
  display: inline-block;
  width: 160px;
  height: 10px;
  background: var(--color-bar-empty);
  border-radius: 5px;
  overflow: hidden;
  vertical-align: middle;
}
.bar .fill { display: block; height: 100%; background: var(--color-bar-fill); }
.bar.critical .fill { background: var(--color-critical); }
.bar.warning  .fill { background: var(--color-warning); }
.bar.ok       .fill { background: var(--color-success); }
.split {
  display: inline-flex;
  width: 160px;
  height: 10px;
  background: var(--color-bar-empty);
  border-radius: 5px;
  overflow: hidden;
  vertical-align: middle;
  margin-right: 8px;
}
.split-read  { background: var(--color-success); }
.split-write { background: var(--color-info); }
.split-other { background: var(--color-muted); }
.legend { font-size: 0.9em; color: var(--color-muted); }
.sw {
  display: inline-block;
  width: 8px; height: 8px;
  border-radius: 2px;
  margin-right: 4px;
  vertical-align: baseline;
}
.sw-read  { background: var(--color-success); }
.sw-write { background: var(--color-info); }
.sw-other { background: var(--color-muted); }
.rec {
  margin: 12px 0;
  padding: 10px 14px;
  border-left: 4px solid var(--color-muted);
  background: color-mix(in srgb, var(--color-bg) 94%, var(--color-text));
  border-radius: 4px;
}
.rec.critical { border-left-color: var(--color-critical); }
.rec.warning  { border-left-color: var(--color-warning); }
.rec.info     { border-left-color: var(--color-info); }
.rec .sev { font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; font-size: 0.75em; margin-right: 8px; }
.rec.critical .sev { color: var(--color-critical); }
.rec.warning  .sev { color: var(--color-warning); }
.rec.info     .sev { color: var(--color-info); }
.rec .action { color: var(--color-muted); font-size: 0.9em; margin-top: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
@media print {
  body { max-width: none; padding: 0; }
  nav { display: none; }
  section { page-break-inside: avoid; }
}
</style>
</head>
<body>
<header>
  <h1>{{ .Title }}</h1>
  <div class="meta">
    <p>PostgreSQL {{ .Meta.ServerVersion }}</p>
    <p>Snapshot: {{ .Meta.SnapshotAt.UTC.Format "2006-01-02 15:04:05 UTC" }}</p>
    {{ if not .Meta.StatsSince.IsZero }}
      <p>Stats since: {{ .Meta.StatsSince.UTC.Format "2006-01-02 15:04:05 UTC" }}</p>
    {{ end }}
    <p>Schema: {{ .Meta.SchemaVersion }} · Agent: {{ .Meta.DBAgentVersion }}</p>
  </div>
</header>
<nav>
  <a href="#overview">Overview</a>
  {{ range .Sections }}<a href="#{{ .ID }}">{{ .Title }}</a>{{ end }}
  {{ if .Recommendations }}<a href="#recommendations">Recommendations</a>{{ end }}
</nav>

<section id="overview">
<h2>Overview</h2>
<table>
<tr><th>Total DB time</th><td class="num">{{ ms .Overview.TotalTimeMs }}</td></tr>
<tr><th>Executions</th><td class="num">{{ commas .Overview.TotalExecutions }} across {{ commas .Overview.TotalQueries }} unique queries</td></tr>
{{ if gt .Overview.TotalTimeMs 0.0 }}
<tr><th>Read/write</th><td>
  <span class="split">
    <span class="split-read" style="width: {{ ratioToWidth .Overview.ReadShare }}"></span><span class="split-write" style="width: {{ ratioToWidth .Overview.WriteShare }}"></span><span class="split-other" style="width: {{ ratioToWidth .Overview.OtherShare }}"></span>
  </span>
  <span class="legend"><span class="sw sw-read"></span> {{ pctInt .Overview.ReadShare }} reads &middot; <span class="sw sw-write"></span> {{ pctInt .Overview.WriteShare }} writes &middot; <span class="sw sw-other"></span> {{ pctInt .Overview.OtherShare }} other</span>
</td></tr>
{{ end }}
{{ if ge .Overview.CacheHitRatio 0.0 }}
<tr><th>Cache hit</th><td>
  <span class="bar {{ .Overview.CacheHitSeverity }}"><span class="fill" style="width: {{ ratioToWidth .Overview.CacheHitRatio }}"></span></span>
  {{ pct .Overview.CacheHitRatio }}
</td></tr>
{{ end }}
</table>
</section>

{{ range .Sections }}
{{ if .Rows }}
<section id="{{ .ID }}">
<h2>{{ .Title }}</h2>
<table>
<thead><tr>
  <th>#</th><th>Calls</th><th>Mean</th><th>Total</th><th>Reads</th><th>Hits</th><th>Cache</th><th>Share</th><th>Query</th>
</tr></thead>
<tbody>
{{ range .Rows }}
<tr>
  <td class="num">{{ .Rank }}</td>
  <td class="num">{{ if .Calls }}{{ commas .Calls }}{{ end }}</td>
  <td class="num">{{ if .MeanMs }}{{ ms .MeanMs }}{{ end }}</td>
  <td class="num">{{ ms .TotalMs }}</td>
  <td class="num">{{ if .Reads }}{{ commas .Reads }}{{ end }}</td>
  <td class="num">{{ if .Hits }}{{ commas .Hits }}{{ end }}</td>
  <td class="num">{{ pct .CacheHit }}</td>
  <td class="num">{{ if .Share }}{{ pctInt .Share }}{{ end }}</td>
  <td class="query">{{ .Query }}</td>
</tr>
{{ end }}
</tbody>
</table>
</section>
{{ end }}
{{ end }}

{{ if .Recommendations }}
<section id="recommendations">
<h2>Recommendations</h2>
{{ range .Recommendations }}
<div class="rec {{ .Severity }}">
  <span class="sev">{{ .Severity }}</span><strong>{{ .Title }}</strong>
  <div>{{ .Message }}</div>
  {{ if .Action }}<div class="action">{{ .Action }}</div>{{ end }}
</div>
{{ end }}
</section>
{{ end }}

</body>
</html>`

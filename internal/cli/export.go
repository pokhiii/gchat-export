package cli

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/OWNER/gchat-export/internal/daterange"
	"github.com/OWNER/gchat-export/internal/export"
	"github.com/spf13/cobra"
)

type exportFlags struct {
	space, since, until, tz, format, maxSize, out string
	attachments, resume, force                    bool
	rps                                           float64
}

func newExportCmd(a *app) *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export one space's messages for a time range",
		Example: `  gchat-export export --space "Team Room" --since 2026-09-01 --until 2026-10-01
  gchat-export export --space spaces/AAAA --since 2026-09-01T09:00:00Z --attachments`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runExport(cmd, f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.space, "space", "", "space resource name (spaces/…) or exact display name (required)")
	fl.StringVar(&f.since, "since", "", "start, inclusive: YYYY-MM-DD or RFC 3339 (required)")
	fl.StringVar(&f.until, "until", "", "end, exclusive: YYYY-MM-DD or RFC 3339 (default now)")
	fl.StringVar(&f.tz, "tz", "", "time zone for dates and display, e.g. Asia/Kolkata (default local)")
	fl.StringVar(&f.format, "format", "md,jsonl", "output formats: md, jsonl or md,jsonl")
	fl.BoolVar(&f.attachments, "attachments", false, "download uploaded attachments")
	fl.StringVar(&f.maxSize, "max-attachment-size", "100MB", "skip attachments larger than this")
	fl.StringVar(&f.out, "out", "", "output root directory (default ./export)")
	fl.BoolVar(&f.resume, "resume", false, "continue an interrupted export")
	fl.BoolVar(&f.force, "force", false, "replace an existing export for the same space and range")
	fl.Float64Var(&f.rps, "rps", 10, "maximum API requests per second")
	return cmd
}

func (a *app) runExport(cmd *cobra.Command, f exportFlags) error {
	opt, err := a.exportOptions(cmd, f)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	c, cred, err := a.chatClient(ctx)
	if err != nil {
		return err
	}
	space, err := c.ResolveSpace(ctx, f.space)
	if err != nil {
		return err
	}
	opt.Space, opt.Scopes, opt.Version = space, cred.Scopes, a.version
	if f.resume && f.until == "" {
		// The first run's end was "now" back then; reuse it, not today's now.
		to, err := export.FindResumeTo(opt.OutRoot, space, opt.Range.From)
		if err != nil {
			return err
		}
		opt.Range.To = to
	}
	res, err := export.Run(ctx, c, opt, a.log)
	if err != nil {
		if res.Dir != "" && !errors.Is(err, export.ErrOutputExists) {
			fmt.Fprintf(a.stderr, "Export interrupted; continue with --resume (output: %s)\n", res.Dir)
		}
		return err
	}
	fmt.Fprintf(a.stdout, "Exported %d messages and %d attachments to %s\n", res.Messages, res.Attachments, res.Dir)
	if res.Partial() {
		fmt.Fprintf(a.stderr, "%d items failed; see errors in %s/manifest.json\n", len(res.Errors), res.Dir)
		return errPartial
	}
	return nil
}

// exportOptions validates flags before any credential or network access.
func (a *app) exportOptions(cmd *cobra.Command, f exportFlags) (export.Options, error) {
	var opt export.Options
	switch {
	case f.space == "":
		return opt, errors.New("--space is required")
	case f.since == "":
		return opt, errors.New("--since is required")
	case f.resume && f.force:
		return opt, errors.New("--resume and --force cannot be used together")
	}
	tz := f.tz
	if tz == "" {
		tz = a.cfg.TZ
	}
	loc := time.Local
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return opt, fmt.Errorf("--tz: unknown time zone %q", tz)
		}
		loc = l
	}
	r, err := daterange.Parse(f.since, f.until, loc, time.Now())
	if err != nil {
		return opt, err
	}
	md, jl, err := parseFormats(f.format)
	if err != nil {
		return opt, err
	}
	maxSize, err := ParseSize(f.maxSize)
	if err != nil {
		return opt, fmt.Errorf("--max-attachment-size: %w", err)
	}
	if cmd.Flags().Changed("rps") {
		if f.rps <= 0 {
			return opt, errors.New("--rps must be greater than 0")
		}
		a.cfg.RPS = f.rps
	}
	out := a.cfg.OutDir
	if f.out != "" {
		out = f.out
	}
	return export.Options{
		Range: r, Markdown: md, JSONL: jl, Attachments: f.attachments, MaxAttachmentSize: maxSize,
		OutRoot: out, Resume: f.resume, Force: f.force,
	}, nil
}

func parseFormats(s string) (md, jsonl bool, err error) {
	for _, p := range strings.Split(s, ",") {
		switch strings.TrimSpace(p) {
		case "md":
			md = true
		case "jsonl":
			jsonl = true
		default:
			return false, false, fmt.Errorf("--format: want md, jsonl or md,jsonl (got %q)", termSafe(s))
		}
	}
	return md, jsonl, nil
}

var sizeRE = regexp.MustCompile(`^(\d+)\s*([A-Za-z]*)$`)

var sizeUnits = map[string]int64{
	"": 1, "b": 1,
	"kb": 1_000, "mb": 1_000_000, "gb": 1_000_000_000,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30,
}

// ParseSize parses sizes like 100MB, 5MiB or 123 (bytes).
func ParseSize(s string) (int64, error) {
	m := sizeRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size %q", termSafe(s))
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q", m[2])
	}
	if n <= 0 || n > math.MaxInt64/unit {
		return 0, fmt.Errorf("size %q out of range", s)
	}
	return n * unit, nil
}

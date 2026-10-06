// Package daterange parses --since/--until/--tz into a half-open UTC range.
package daterange

import (
	"errors"
	"fmt"
	"time"
)

// ErrEmptyRange is returned when since is not before until.
var ErrEmptyRange = errors.New("--since must be before --until")

// Range is the half-open interval [From, To), both in UTC. Loc is the zone
// used to interpret dates and display times.
type Range struct {
	From, To time.Time
	Loc      *time.Location
	DateOnly bool
}

const dateLayout = "2006-01-02"

// Parse interprets since and until as YYYY-MM-DD (midnight in loc) or RFC 3339.
// An empty until means now.
func Parse(since, until string, loc *time.Location, now time.Time) (Range, error) {
	if since == "" {
		return Range{}, errors.New("--since is required")
	}
	from, fromDate, err := parseOne("--since", since, loc)
	if err != nil {
		return Range{}, err
	}
	to, toDate := now, false
	if until != "" {
		if to, toDate, err = parseOne("--until", until, loc); err != nil {
			return Range{}, err
		}
	}
	if !from.Before(to) {
		return Range{}, ErrEmptyRange
	}
	return Range{From: from.UTC(), To: to.UTC(), Loc: loc, DateOnly: fromDate && toDate}, nil
}

func parseOne(flag, s string, loc *time.Location) (time.Time, bool, error) {
	if t, err := time.ParseInLocation(dateLayout, s, loc); err == nil {
		return t, true, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, false, nil
	}
	return time.Time{}, false, fmt.Errorf("%s %q: want YYYY-MM-DD or RFC 3339 timestamp", flag, s)
}

// DirName is a filesystem-safe name for the range.
func (r Range) DirName() string {
	if r.DateOnly {
		return r.From.In(r.Loc).Format(dateLayout) + "_" + r.To.In(r.Loc).Format(dateLayout)
	}
	const compact = "20060102T150405Z"
	return r.From.UTC().Format(compact) + "_" + r.To.UTC().Format(compact)
}

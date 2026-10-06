package daterange

import (
	"errors"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func utc(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}

var now = utc("2026-10-06T12:00:00Z")

func TestParseDateOnlyLocal(t *testing.T) {
	r, err := Parse("2026-09-01", "2026-10-01", mustLoc(t, "Asia/Kolkata"), now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.Equal(utc("2026-08-31T18:30:00Z")) || !r.To.Equal(utc("2026-09-30T18:30:00Z")) {
		t.Fatalf("got %v – %v", r.From, r.To)
	}
	if !r.DateOnly {
		t.Fatal("DateOnly = false")
	}
	if r.From.Location() != time.UTC {
		t.Fatal("From not UTC")
	}
}

func TestParseDSTStart(t *testing.T) {
	r, err := Parse("2026-03-08", "2026-03-09", mustLoc(t, "America/New_York"), now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.Equal(utc("2026-03-08T05:00:00Z")) || !r.To.Equal(utc("2026-03-09T04:00:00Z")) {
		t.Fatalf("got %v – %v", r.From, r.To)
	}
}

func TestParseRFC3339(t *testing.T) {
	r, err := Parse("2026-09-01T10:00:00+02:00", "2026-09-02", time.UTC, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.From.Equal(utc("2026-09-01T08:00:00Z")) {
		t.Fatalf("From = %v", r.From)
	}
	if r.DateOnly {
		t.Fatal("DateOnly = true")
	}
}

func TestParseUntilDefaultsNow(t *testing.T) {
	r, err := Parse("2026-09-01", "", time.UTC, now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.To.Equal(now) || r.DateOnly {
		t.Fatalf("To = %v, DateOnly = %v", r.To, r.DateOnly)
	}
}

func TestParseRejects(t *testing.T) {
	for _, c := range [][2]string{{"", "2026-09-01"}, {"2026-13-01", ""}, {"yesterday", ""}} {
		if _, err := Parse(c[0], c[1], time.UTC, now); err == nil {
			t.Errorf("Parse(%q, %q) succeeded", c[0], c[1])
		}
	}
	for _, c := range [][2]string{{"2026-09-02", "2026-09-01"}, {"2026-09-01", "2026-09-01"}} {
		if _, err := Parse(c[0], c[1], time.UTC, now); !errors.Is(err, ErrEmptyRange) {
			t.Errorf("Parse(%q, %q) err = %v, want ErrEmptyRange", c[0], c[1], err)
		}
	}
}

func TestDirName(t *testing.T) {
	r, _ := Parse("2026-09-01", "2026-10-01", mustLoc(t, "Asia/Kolkata"), now)
	if got := r.DirName(); got != "2026-09-01_2026-10-01" {
		t.Fatalf("date-only DirName = %q", got)
	}
	r, _ = Parse("2026-09-01T09:30:00Z", "2026-10-01T00:00:00Z", time.UTC, now)
	if got := r.DirName(); got != "20260901T093000Z_20261001T000000Z" {
		t.Fatalf("timestamp DirName = %q", got)
	}
}

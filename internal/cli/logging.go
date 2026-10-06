package cli

import (
	"context"
	"log/slog"
	"regexp"
)

var secretPatterns = regexp.MustCompile(
	`ya29\.[\w.-]+` + // Google access tokens
		`|1//[\w.-]+` + // Google refresh tokens
		`|Bearer\s+\S+` +
		`|"(?:access|refresh|id)_token"\s*:\s*"[^"]+"`)

func redact(s string) string { return secretPatterns.ReplaceAllString(s, "[REDACTED]") }

type redactingHandler struct{ next slog.Handler }

// NewRedactingHandler scrubs token-shaped values from log messages and
// string-valued attributes before they reach h.
func NewRedactingHandler(h slog.Handler) slog.Handler { return redactingHandler{next: h} }

func (h redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = redactAttr(a)
	}
	return redactingHandler{next: h.next.WithAttrs(clean)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redact(v.String()))
	case slog.KindGroup:
		group := v.Group()
		clean := make([]any, len(group))
		for i, g := range group {
			clean[i] = redactAttr(g)
		}
		return slog.Group(a.Key, clean...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, redact(err.Error()))
		}
		return slog.String(a.Key, redact(v.String()))
	}
	return slog.Attr{Key: a.Key, Value: v}
}

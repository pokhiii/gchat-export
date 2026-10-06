package export

import (
	"encoding/json"
	"time"

	"github.com/OWNER/gchat-export/internal/fsutil"
	"github.com/OWNER/gchat-export/internal/model"
)

const manifestFile = "manifest.json"

type manifestAttachment struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type manifest struct {
	ToolVersion string               `json:"tool_version"`
	GeneratedAt time.Time            `json:"generated_at"`
	Space       model.Space          `json:"space"`
	Range       manifestRange        `json:"range"`
	Scopes      []string             `json:"scopes"`
	Messages    int                  `json:"messages"`
	Attachments []manifestAttachment `json:"attachments"`
	Errors      []ItemError          `json:"errors"`
}

type manifestRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	TZ   string    `json:"tz"`
}

func writeManifest(dir string, opt Options, st *state, generatedAt time.Time) error {
	m := manifest{
		ToolVersion: opt.Version,
		GeneratedAt: generatedAt.UTC(),
		Space:       opt.Space,
		Range:       manifestRange{From: opt.Range.From, To: opt.Range.To, TZ: opt.Range.Loc.String()},
		Scopes:      opt.Scopes,
		Messages:    st.Messages,
		Attachments: st.Attachments,
		Errors:      st.Errors,
	}
	if m.Attachments == nil {
		m.Attachments = []manifestAttachment{}
	}
	if m.Errors == nil {
		m.Errors = []ItemError{}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(dir, manifestFile, append(b, '\n'))
}

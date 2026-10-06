package export

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pokhiii/gchat-export/internal/fsutil"
)

const stateFile = ".state.json"

// state is the resume checkpoint, written atomically after every page.
type state struct {
	Space       string               `json:"space"`
	From        time.Time            `json:"from"`
	To          time.Time            `json:"to"`
	PageToken   string               `json:"page_token"`
	Complete    bool                 `json:"complete"` // listing finished; only the final files remain
	JSONLBytes  int64                `json:"jsonl_bytes"`
	Messages    int                  `json:"messages"`
	Attachments []manifestAttachment `json:"attachments"`
	Errors      []ItemError          `json:"errors"`
}

func loadState(dir string) (*state, error) {
	if err := fsutil.CheckPrivate(filepath.Join(dir, stateFile)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoState
		}
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, stateFile)) // #nosec G304 -- inside the export dir
	if err != nil {
		return nil, err
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, errors.New("resume state is corrupt; re-run with --force")
	}
	return &s, nil
}

func (s *state) save(dir string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(dir, stateFile, b)
}

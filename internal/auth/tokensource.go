package auth

import (
	"context"
	"sync"

	"golang.org/x/oauth2"
)

type persistingSource struct {
	mu    sync.Mutex
	src   oauth2.TokenSource
	cred  *Credential
	store Store
}

// PersistingTokenSource refreshes tokens as needed and saves each new token
// to store.
func PersistingTokenSource(ctx context.Context, cfg *oauth2.Config, cred *Credential, store Store) oauth2.TokenSource {
	return &persistingSource{src: cfg.TokenSource(ctx, cred.Token), cred: cred, store: store}
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != p.cred.Token.AccessToken {
		updated := *p.cred
		updated.Token = tok
		if err := p.store.Save(&updated); err != nil {
			return nil, err
		}
		p.cred = &updated
	}
	return tok, nil
}

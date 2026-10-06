// Package chat wraps the Google Chat API and converts its types to model.
package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/OWNER/gchat-export/internal/daterange"
	"github.com/OWNER/gchat-export/internal/model"
	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

const pageSize = 1000

// Client is a read-only Chat API client.
type Client struct {
	svc *chatapi.Service
}

// New builds a client on hc, which must already carry OAuth credentials.
// An empty endpoint means the production API.
func New(ctx context.Context, hc *http.Client, endpoint string) (*Client, error) {
	opts := []option.ClientOption{option.WithHTTPClient(hc)}
	if endpoint != "" {
		opts = append(opts, option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"))
	}
	svc, err := chatapi.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

// Filter is the messages.list filter for r: create_time in [From, To). The API
// documents only > and <, so the inclusive lower bound is expressed as
// "> From - 1µs" (Chat timestamps have microsecond precision).
func Filter(r daterange.Range) string {
	from := r.From.Add(-time.Microsecond).UTC().Format("2006-01-02T15:04:05.000000Z07:00")
	return fmt.Sprintf(`create_time > %q AND create_time < %q`, from, r.To.UTC().Format(time.RFC3339Nano))
}

// ListSpaces returns every space the user is a member of.
func (c *Client) ListSpaces(ctx context.Context) ([]model.Space, error) {
	var out []model.Space
	err := c.svc.Spaces.List().PageSize(pageSize).Pages(ctx, func(resp *chatapi.ListSpacesResponse) error {
		for _, s := range resp.Spaces {
			out = append(out, convertSpace(s))
		}
		return nil
	})
	return out, Classify(err)
}

// ResolveSpace finds a space by Google Chat URL, resource name ("spaces/…"),
// exact display name, or, if no display name matches, bare space ID.
func (c *Client) ResolveSpace(ctx context.Context, ref string) (model.Space, error) {
	if looksLikeURL(ref) {
		name, ok := ParseSpaceRef(ref)
		if !ok {
			return model.Space{}, ErrBadChatURL
		}
		return c.getSpace(ctx, name)
	}
	if strings.HasPrefix(ref, "spaces/") {
		return c.getSpace(ctx, ref)
	}
	all, err := c.ListSpaces(ctx)
	if err != nil {
		return model.Space{}, err
	}
	var matches []model.Space
	for _, s := range all {
		if s.DisplayName == ref {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		if bareIDRE.MatchString(ref) {
			return c.getBareID(ctx, ref)
		}
		return model.Space{}, ErrSpaceNotFound
	case 1:
		return matches[0], nil
	}
	return model.Space{}, &AmbiguousSpaceError{Candidates: matches}
}

// getBareID tries ref as a space ID after no display name matched. Spaces the
// user cannot see may answer 400/403 rather than 404, and ref was most likely
// a mistyped name, so those read as not found; auth and setup errors pass
// through unchanged.
func (c *Client) getBareID(ctx context.Context, ref string) (model.Space, error) {
	s, err := c.getSpace(ctx, "spaces/"+ref)
	if err == nil {
		return s, nil
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) && (ge.Code == 400 || ge.Code == 403 || ge.Code == 404) {
		return model.Space{}, ErrSpaceNotFound
	}
	return model.Space{}, err
}

func (c *Client) getSpace(ctx context.Context, name string) (model.Space, error) {
	s, err := c.svc.Spaces.Get(name).Context(ctx).Do()
	if err != nil {
		return model.Space{}, Classify(err)
	}
	return convertSpace(s), nil
}

// ListMembers maps users/ID to display name for the space's human members.
func (c *Client) ListMembers(ctx context.Context, space string) (map[string]string, error) {
	out := map[string]string{}
	err := c.svc.Spaces.Members.List(space).PageSize(pageSize).Pages(ctx, func(resp *chatapi.ListMembershipsResponse) error {
		for _, m := range resp.Memberships {
			if m.Member != nil && m.Member.DisplayName != "" {
				out[m.Member.Name] = m.Member.DisplayName
			}
		}
		return nil
	})
	return out, Classify(err)
}

// ListMessages pages through messages in r, oldest first, starting at
// pageToken, calling fn with each page and the token of the next page ("" on
// the last page).
func (c *Client) ListMessages(ctx context.Context, space string, r daterange.Range, pageToken string, members map[string]string,
	fn func(page []model.Message, nextPageToken string) error) error {
	for {
		resp, err := c.svc.Spaces.Messages.List(space).
			Filter(Filter(r)).
			OrderBy("createTime ASC").
			PageSize(pageSize).
			ShowDeleted(false).
			PageToken(pageToken).
			Context(ctx).Do()
		if err != nil {
			return Classify(err)
		}
		page := make([]model.Message, 0, len(resp.Messages))
		for _, m := range resp.Messages {
			page = append(page, convertMessage(m, members))
		}
		if err := fn(page, resp.NextPageToken); err != nil {
			return err
		}
		if resp.NextPageToken == "" {
			return nil
		}
		pageToken = resp.NextPageToken
	}
}

// DownloadMedia streams an uploaded attachment's bytes.
func (c *Client) DownloadMedia(ctx context.Context, resourceName string) (io.ReadCloser, error) {
	resp, err := c.svc.Media.Download(resourceName).Context(ctx).Download()
	if err != nil {
		return nil, Classify(err)
	}
	return resp.Body, nil
}

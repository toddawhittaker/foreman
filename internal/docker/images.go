package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Image is the part of GET /images/{name}/json Foreman uses.
type Image struct {
	ID          string
	RepoTags    []string
	RepoDigests []string
}

// InspectImage reads one image by reference. A missing one is ErrNotFound.
func (c *Client) InspectImage(ctx context.Context, ref string) (Image, error) {
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/images/" + ref + "/json"})
	if err != nil {
		return Image{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID          string `json:"Id"`
		RepoTags    []string
		RepoDigests []string
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Image{}, fmt.Errorf("could not read image %s from the Docker daemon: %w", ref, err)
	}
	return Image(raw), nil
}

// pullLimit is how long one image pull may take.
const pullLimit = 30 * time.Minute

// PullImage downloads an image. ref must carry a tag or a digest, so the
// daemon never pulls every tag of a repository (SPEC 14.3). The daemon
// reports many failures inside a 200 reply; those come back as errors too.
func (c *Client) PullImage(ctx context.Context, ref string) error {
	if !hasTagOrDigest(ref) {
		return fmt.Errorf("image %q names no tag or digest, so Foreman will not pull it", ref)
	}
	ctx, cancel := context.WithTimeout(ctx, pullLimit)
	defer cancel()
	resp, err := c.do(ctx, request{
		method: http.MethodPost, path: "/images/create",
		query: url.Values{"fromImage": {ref}}, long: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Error       string
			ErrorDetail struct{ Message string }
		}
		err := dec.Decode(&msg)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the pull of %s ended badly: %w", ref, err)
		}
		if msg.Error != "" || msg.ErrorDetail.Message != "" {
			text := msg.ErrorDetail.Message
			if text == "" {
				text = msg.Error
			}
			return fmt.Errorf("the Docker daemon could not pull %s: %s", ref, text)
		}
	}
}

// hasTagOrDigest reports whether the last path part of ref has a ":tag" or
// an "@digest". A colon earlier on is a registry port.
func hasTagOrDigest(ref string) bool {
	last := ref[strings.LastIndex(ref, "/")+1:]
	return strings.ContainsAny(last, ":@")
}

package docker_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

func TestInspectImage(t *testing.T) {
	c, d := newClient(t)
	ctx := context.Background()
	if _, err := c.InspectImage(ctx, testImage); !errors.Is(err, docker.ErrNotFound) {
		t.Fatalf("missing image: want ErrNotFound, got %v", err)
	}
	d.AddImage(testImage)
	img, err := c.InspectImage(ctx, testImage)
	if err != nil || !strings.HasPrefix(img.ID, "sha256:") || len(img.RepoTags) != 1 {
		t.Fatalf("InspectImage = %+v, %v", img, err)
	}
	// A reference with a registry port and a digest still reaches the daemon whole.
	ref := "registry.example:5000/factoriotools/factorio@sha256:" + strings.Repeat("a", 64)
	d.AddImage(ref)
	if _, err := c.InspectImage(ctx, ref); err != nil {
		t.Errorf("digest reference: %v", err)
	}
}

func TestPullImageAddsIt(t *testing.T) {
	c, d := newClient(t)
	ctx := context.Background()
	if err := c.PullImage(ctx, testImage); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InspectImage(ctx, testImage); err != nil {
		t.Errorf("image missing after pull: %v", err)
	}
	if got := d.Pulls(); len(got) != 1 || got[0] != testImage {
		t.Errorf("pulls = %v", got)
	}
}

func TestPullErrorInsideA200IsAnError(t *testing.T) {
	c, d := newClient(t)
	d.FailPull(testImage, "manifest for "+testImage+" not found")
	err := c.PullImage(context.Background(), testImage)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want the daemon's reason, got %v", err)
	}
	if _, err := c.InspectImage(context.Background(), testImage); !errors.Is(err, docker.ErrNotFound) {
		t.Errorf("a failed pull left the image behind: %v", err)
	}
}

func TestPullRefusesAReferenceWithoutTagOrDigest(t *testing.T) {
	c, d := newClient(t)
	for _, ref := range []string{"factoriotools/factorio", "registry.example:5000/factorio", ""} {
		if err := c.PullImage(context.Background(), ref); err == nil {
			t.Errorf("pull of %q was allowed", ref)
		}
	}
	if len(d.Calls()) != 0 {
		t.Errorf("refused pulls reached the daemon: %v", d.Calls())
	}
	for _, ref := range []string{"a/b:1", "registry.example:5000/a/b:1", "a/b@sha256:" + strings.Repeat("0", 64)} {
		if err := c.PullImage(context.Background(), ref); err != nil {
			t.Errorf("pull of %q: %v", ref, err)
		}
	}
}

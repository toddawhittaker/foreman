package game

import (
	"errors"
	"net/url"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// SPEC 17.9: a failed image download names the image and whether Docker
// said it has no such image, so the page can say what to do next.
func TestPullErrorClassifiesTheFailure(t *testing.T) {
	const ref = "factoriotools/factorio:9.9.9"
	notFound := &docker.APIError{Status: 404, Message: "manifest for " + ref + " not found"}
	other := errors.New("the Docker daemon could not pull " + ref + ": toomanyrequests")

	var de *ImageDownloadError
	if !errors.As(pullError(ref, notFound), &de) || de.Image != ref || !de.NotFound {
		t.Errorf("404: got %+v", de)
	}
	de = nil
	if !errors.As(pullError(ref, other), &de) || de.Image != ref || de.NotFound {
		t.Errorf("other: got %+v", de)
	}
	if !errors.Is(pullError(ref, other), other) {
		t.Error("the cause is not kept for the log")
	}

	// A Docker that answers nothing keeps its own plain sentence.
	gone := &docker.UnreachableError{Target: "unix:///x", Err: &url.Error{Op: "Post", URL: "http://docker", Err: errors.New("dial")}}
	var wrapped *ImageDownloadError
	if errors.As(pullError(ref, gone), &wrapped) {
		t.Errorf("an unreachable Docker was worded as a download failure: %+v", wrapped)
	}
}

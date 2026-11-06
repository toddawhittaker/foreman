package web

import (
	"context"
	"errors"

	"github.com/toddawhittaker/foreman/internal/game"
)

// gamePreviewer makes map previews in a one-off game container (SPEC 14.3).
type gamePreviewer struct{ c *game.Controller }

// GamePreviewer is the MapPreviewer backed by the game controller; nil
// without server control.
func GamePreviewer(c *game.Controller) MapPreviewer {
	if c == nil {
		return nil
	}
	return gamePreviewer{c}
}

func (g gamePreviewer) Preview(ctx context.Context, req PreviewRequest) ([]PreviewImage, error) {
	pngs, err := g.c.Preview(ctx, game.PreviewRequest{
		GameVersion: req.Version, SpaceAge: req.SpaceAge, Preset: req.Preset, Seed: req.Seed,
		MapGenSettings: req.MapGen, MapSettings: req.MapSettings, Planets: req.Planets,
	})
	if err != nil {
		return nil, previewError(err)
	}
	out := make([]PreviewImage, len(pngs))
	for i, p := range pngs {
		out[i] = PreviewImage{Planet: p.Planet, PNG: p.Data}
	}
	return out, nil
}

// previewError turns the game layer's refusals into the ones the page words.
func previewError(err error) error {
	if pe, ok := errors.AsType[*game.PreviewError](err); ok {
		return &PreviewFailure{Log: pe.Log, OOM: pe.OOM}
	}
	switch {
	case errors.Is(err, game.ErrPreviewBusy):
		return ErrPreviewBusy
	case errors.Is(err, game.ErrPreviewImageMissing):
		return ErrPreviewNoImage
	case errors.Is(err, game.ErrPreviewTimeout):
		return ErrPreviewTimeout
	}
	return err
}

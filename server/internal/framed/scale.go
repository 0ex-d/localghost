// Package framed is the engine of the photo pipeline daemon: intake of raw images from the phone,
// archive-untouched storage, preview generation, EXIF extraction, and the daily location path that
// turns photos plus watch points into a journal-ready day.
package framed

import (
	"image"

	"github.com/LocalGhostDao/localghost/server/internal/imgfit"
)

// downscale and applyOrientation live in internal/imgfit (oracled fits the model's images with the
// same code); these names stay for the pipeline and its tests.
func downscale(src image.Image, maxEdge int) image.Image { return imgfit.Downscale(src, maxEdge) }

func applyOrientation(src image.Image, orientation int) image.Image {
	return imgfit.Orient(src, orientation)
}

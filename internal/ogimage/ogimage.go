package ogimage

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io/fs"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
)

// Generator renders dynamic OG images using a base template image and overlaid text.
// It is safe for concurrent use after initialization.
type Generator struct {
	baseImage   image.Image
	boldFont    *truetype.Font
	regularFont *truetype.Font
}

// New creates a Generator by loading the base image and fonts from the given filesystem.
func New(fsys fs.FS) (*Generator, error) {
	f, err := fsys.Open("og-base.png")
	if err != nil {
		return nil, fmt.Errorf("open base image: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	baseImg, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode base image: %w", err)
	}

	boldFont, err := loadFont(fsys, "fonts/Inter-Bold.ttf")
	if err != nil {
		return nil, fmt.Errorf("load bold font: %w", err)
	}

	regularFont, err := loadFont(fsys, "fonts/Inter-Regular.ttf")
	if err != nil {
		return nil, fmt.Errorf("load regular font: %w", err)
	}

	return &Generator{
		baseImage:   baseImg,
		boldFont:    boldFont,
		regularFont: regularFont,
	}, nil
}

// RenderSearch generates an OG image for a search result.
// It draws the search term and match summary on top of the base image.
func (g *Generator) RenderSearch(term string, totalMatches, totalExtensions int, repo string) ([]byte, error) {
	const width, height = 1200, 630

	dc := gg.NewContext(width, height)
	dc.DrawImage(g.baseImage, 0, 0)

	// Semi-transparent overlay for text readability
	dc.SetRGBA(0, 0, 0, 0.4)
	dc.DrawRectangle(0, 300, width, 330)
	dc.Fill()

	// Draw search term (bold, white, wrapped)
	boldFace := truetype.NewFace(g.boldFont, &truetype.Options{Size: 48, DPI: 72})
	dc.SetFontFace(boldFace)
	dc.SetHexColor("#FFFFFF")

	displayTerm := term
	if len(displayTerm) > 60 {
		displayTerm = displayTerm[:57] + "..."
	}
	displayTerm = "\u201c" + displayTerm + "\u201d"

	dc.DrawStringWrapped(displayTerm, 80, 360, 0, 0, 1040, 1.4, gg.AlignLeft)

	// Draw match summary (regular, lighter color)
	regularFace := truetype.NewFace(g.regularFont, &truetype.Options{Size: 32, DPI: 72})
	dc.SetFontFace(regularFace)
	dc.SetHexColor("#B0D0FF")

	summary := fmt.Sprintf("%d matches across %d %s", totalMatches, totalExtensions, repo)
	dc.DrawString(summary, 80, 520)

	// Draw "Veloria" branding
	smallFace := truetype.NewFace(g.regularFont, &truetype.Options{Size: 24, DPI: 72})
	dc.SetFontFace(smallFace)
	dc.SetHexColor("#8090B0")
	dc.DrawString("Veloria - WordPress Code Search", 80, 580)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func loadFont(fsys fs.FS, path string) (*truetype.Font, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f, err := truetype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}


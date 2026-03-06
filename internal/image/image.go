package image

import (
	"bytes"
	"fmt"
	stdimage "image"
	"image/png"
	"io/fs"
	"strings"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
)

// Generator renders dynamic OG images using a base template image and overlaid text.
// It is safe for concurrent use after initialization.
type Generator struct {
	baseImage   stdimage.Image
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

// Layout constants for the OG image.
const (
	ogWidth  = 1200
	ogHeight = 630

	panelX = 60.0
	panelY = 150.0
	panelW = 1080.0
	panelH = 420.0

	textX    = 100.0
	textMaxW = 1000.0
	textTopY = 250.0 // first line baseline

	// Vertical positions for summary and branding (relative to panel bottom).
	summaryY  = panelY + panelH - 75
	brandingY = panelY + panelH - 35

	// Maximum height available for the search term text.
	maxTermHeight = summaryY - 20 - textTopY
)

// Font sizes tried in order from largest to smallest.
var termFontSizes = []float64{48, 40, 34, 28}

// RenderSearch generates an OG image for a search result.
// It draws the search term and match summary on top of the base image.
func (g *Generator) RenderSearch(term string, totalMatches, totalExtensions int, repo string) ([]byte, error) {
	dc := gg.NewContext(ogWidth, ogHeight)
	dc.DrawImage(g.baseImage, 0, 0)

	// Glass panel overlay
	drawGlassPanel(dc, panelX, panelY, panelW, panelH)

	// Prepare display term with smart quotes, cap at 200 runes.
	displayTerm := capRunes(term, 200)
	displayTerm = "\u201c" + displayTerm + "\u201d"

	// Pick the largest font size that fits within the available height.
	fontSize := g.pickTermFontSize(dc, displayTerm, textMaxW, maxTermHeight)

	boldFace := truetype.NewFace(g.boldFont, &truetype.Options{Size: fontSize, DPI: 72})
	dc.SetFontFace(boldFace)
	dc.SetHexColor("#f4f6f9")

	lines := breakText(dc, displayTerm, textMaxW)
	lineHeight := fontSize * 1.4
	maxLines := max(int(maxTermHeight/lineHeight), 1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		// Ensure the closing quote is present on the last visible line.
		last := lines[maxLines-1]
		runes := []rune(last)
		if len(runes) > 3 {
			runes = runes[:len(runes)-3]
		}
		lines[maxLines-1] = string(runes) + "...\u201d"
	}

	for i, line := range lines {
		y := textTopY + float64(i)*lineHeight
		dc.DrawString(line, textX, y)
	}

	// Match summary
	summaryFace := truetype.NewFace(g.regularFont, &truetype.Options{Size: 28, DPI: 72})
	dc.SetFontFace(summaryFace)
	dc.SetHexColor("#00e5ff")

	summary := fmt.Sprintf("%d matches across %d %s", totalMatches, totalExtensions, repo)
	dc.DrawString(summary, textX, summaryY)

	// Branding
	smallFace := truetype.NewFace(g.regularFont, &truetype.Options{Size: 20, DPI: 72})
	dc.SetFontFace(smallFace)
	dc.SetHexColor("#7a8394")
	dc.DrawString("veloria.io", textX, brandingY)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// pickTermFontSize tries decreasing font sizes until the wrapped text
// fits within maxHeight at the given maxWidth.
func (g *Generator) pickTermFontSize(dc *gg.Context, text string, maxWidth, maxHeight float64) float64 {
	for _, size := range termFontSizes {
		face := truetype.NewFace(g.boldFont, &truetype.Options{Size: size, DPI: 72})
		dc.SetFontFace(face)

		lines := breakText(dc, text, maxWidth)
		lineHeight := size * 1.4
		totalHeight := float64(len(lines)) * lineHeight

		if totalHeight <= maxHeight {
			return size
		}
	}
	return termFontSizes[len(termFontSizes)-1] // smallest
}

// breakText splits text into lines that fit within maxWidth.
// It breaks at whitespace when possible and mid-word when a single
// token exceeds the line width (common for regex patterns).
func breakText(dc *gg.Context, text string, maxWidth float64) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	currentLine := ""

	for _, word := range words {
		ww, _ := dc.MeasureString(word)
		if ww > maxWidth {
			// Flush current line before breaking the long word.
			if currentLine != "" {
				lines = append(lines, currentLine)
				currentLine = ""
			}
			lines = append(lines, breakWord(dc, word, maxWidth)...)
			continue
		}

		test := currentLine
		if test != "" {
			test += " "
		}
		test += word

		tw, _ := dc.MeasureString(test)
		if tw > maxWidth && currentLine != "" {
			lines = append(lines, currentLine)
			currentLine = word
		} else {
			currentLine = test
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// breakWord splits a single long word into chunks that each fit within maxWidth.
func breakWord(dc *gg.Context, word string, maxWidth float64) []string {
	var lines []string
	runes := []rune(word)
	start := 0

	for start < len(runes) {
		end := start + 1
		for end <= len(runes) {
			w, _ := dc.MeasureString(string(runes[start:end]))
			if w > maxWidth {
				break
			}
			end++
		}
		end--
		if end <= start {
			end = start + 1 // at least one rune per chunk
		}
		lines = append(lines, string(runes[start:end]))
		start = end
	}

	return lines
}

// drawGlassPanel draws a rounded, semi-transparent panel with neon accent lines.
func drawGlassPanel(dc *gg.Context, x, y, w, h float64) {
	r := 20.0

	// Fill
	dc.DrawRoundedRectangle(x, y, w, h, r)
	dc.SetRGBA(0.047, 0.055, 0.11, 0.7)
	dc.Fill()

	// Border
	dc.DrawRoundedRectangle(x, y, w, h, r)
	dc.SetRGBA(1, 1, 1, 0.10)
	dc.SetLineWidth(1.5)
	dc.Stroke()

	// Cyan accent at top-left
	dc.SetLineWidth(2)
	dc.SetHexColor("#00e5ff")
	dc.DrawLine(x+30, y, x+90, y)
	dc.Stroke()

	// Pink accent at top-right
	dc.SetHexColor("#ff2d7b")
	dc.DrawLine(x+w-70, y, x+w-30, y)
	dc.Stroke()
}

// capRunes truncates s to at most n runes, appending "..." if shortened.
func capRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
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

package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
)

// Each width and format here needs a matching imgproxy preset and nginx rule.
var renditionWidths = []int{640, 960, 1280, 1920, 2560}

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

type albumItem struct {
	Num           string
	File          string
	Video         bool
	Description   string
	Width, Height int
	AVIF, WebP    string
	Sizes         string
}

type albumView struct {
	Title string
	Album bool
	Count int
	Items []albumItem
}

func isVideo(file string) bool {
	ext := filepath.Ext(file)
	return ext == ".mp4" || ext == ".webm"
}

// imgproxy never enlarges, so the first rendition at least as wide as the file comes back at the file's own width.
func srcset(file, format string, width int) string {
	var parts []string
	for _, w := range renditionWidths {
		parts = append(parts, fmt.Sprintf("/r/%d/%s/%s %dw", w, format, file, min(w, width)))
		if w >= width {
			break
		}
	}
	return strings.Join(parts, ", ")
}

func renderAlbum(p Post) ([]byte, error) {
	v := albumView{Title: p.Title, Album: len(p.Items) > 1, Count: len(p.Items)}
	for i, it := range p.Items {
		v.Items = append(v.Items, albumItem{
			Num:         fmt.Sprintf("%02d", i+1),
			File:        it.File,
			Video:       isVideo(it.File),
			Description: it.Description,
			Width:       it.Width,
			Height:      it.Height,
		})
		if it.Width > 0 && it.Height > 0 && !it.Animated {
			last := &v.Items[len(v.Items)-1]
			last.AVIF, last.WebP = srcset(it.File, "avif", it.Width), srcset(it.File, "webp", it.Width)
			shown := fmt.Sprintf("%dpx,calc(92vh*%d/%d)", it.Width, it.Width, it.Height)
			last.Sizes = "(max-width:900px) min(calc(100vw - 32px)," + shown + "), min(800px,calc(100vw - 480px)," + shown + ")"
		}
	}
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "album.html", v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *App) BuildPage(p Post) error {
	page, err := renderAlbum(p)
	if err != nil {
		return err
	}
	return a.store.WriteAtomic(a.store.PagePath(p.Token), page)
}

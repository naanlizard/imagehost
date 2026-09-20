package main

import (
	"strings"
	"testing"
)

const (
	fileA = "00000000000000000000000000000001.jpg"
	fileB = "00000000000000000000000000000002.mp4"
	fileC = "00000000000000000000000000000003.png"
)

func TestRenderAlbumEscapesAndNumbers(t *testing.T) {
	p := Post{Token: newToken(), Title: `"><img src=x onerror=alert(1)>`, Items: []Item{
		{File: fileB, Description: "clip"},
		{File: fileA, Description: "<script>alert(1)</script>\nsecond line", Width: 4032, Height: 3024},
	}}
	page, err := renderAlbum(p)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, bad := range []string{"<script>alert", "<img src=x", "og:image"} {
		if strings.Contains(html, bad) {
			t.Errorf("unescaped %q", bad)
		}
	}
	for _, want := range []string{
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		`<div class="num">01</div>`, `<div class="num">02</div>`,
		`<video controls preload="metadata" src="/i/` + fileB + `">`,
		`<a href="/i/` + fileA + `" target="_blank" rel="noopener"><picture><source type="image/avif" srcset="/r/640/avif/` + fileA + ` 640w, `,
		`/r/2560/webp/` + fileA + ` 2560w" sizes="(max-width:900px) min(calc(100vw - 32px),4032px,calc(92vh*4032/3024)), min(800px,calc(100vw - 480px),4032px,calc(92vh*4032/3024))">`,
		`<img loading="lazy" src="/i/` + fileA + `" alt="" width="4032" height="3024" style="width:min(100%,4032px,calc(92vh*4032/3024))"></picture></a>`,
		"open video file", "2 items", `<link rel="icon" href="data:image/svg+xml,`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestSrcsetStopsAtTheFileWidth(t *testing.T) {
	for width, want := range map[int]string{
		500:  "/r/640/webp/f 500w",
		1280: "/r/640/webp/f 640w, /r/960/webp/f 960w, /r/1280/webp/f 1280w",
		1500: "/r/640/webp/f 640w, /r/960/webp/f 960w, /r/1280/webp/f 1280w, /r/1920/webp/f 1500w",
		9000: "/r/640/webp/f 640w, /r/960/webp/f 960w, /r/1280/webp/f 1280w, /r/1920/webp/f 1920w, /r/2560/webp/f 2560w",
	} {
		if got := srcset("f", "webp", width); got != want {
			t.Errorf("%d: %q", width, got)
		}
	}
	page, _ := renderAlbum(Post{Token: newToken(), Items: []Item{{File: fileC, Width: 10, Height: 10, Animated: true}}})
	if strings.Contains(string(page), "<picture>") {
		t.Error("animated image got still renditions")
	}
}

func TestRenderSingleHasNoHeaderOrNumber(t *testing.T) {
	page, err := renderAlbum(Post{Token: newToken(), Title: "T", Items: []Item{{File: fileC}}})
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, bad := range []string{"<h1>", `class="num"`, "items</div>", ` width="`, "style=", "<picture>"} {
		if strings.Contains(html, bad) {
			t.Errorf("single page has %q", bad)
		}
	}
	if !strings.Contains(html, "<title>T</title>") {
		t.Error("title tag missing")
	}
}

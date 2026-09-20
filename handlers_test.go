package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 64)...)
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{2}, 64)...)
)

type upFile struct {
	name string
	data []byte
}

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &App{store: store, baseURL: "https://img.test"}, dir
}

func uploadReq(fields map[string]string, files ...upFile) *http.Request {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for _, f := range files {
		w, _ := mw.CreateFormFile("files", f.name)
		w.Write(f.data)
	}
	mw.Close()
	r := httptest.NewRequest("POST", "/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func do(a *App, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	return w
}

func mustUpload(t *testing.T, a *App, fields map[string]string, files ...upFile) Post {
	t.Helper()
	w := do(a, uploadReq(fields, files...))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	token := strings.TrimPrefix(w.Header().Get("Location"), "/edit/")
	p, err := a.store.Get(token)
	if err != nil {
		t.Fatalf("post %q: %v", token, err)
	}
	return p
}

func postForm(a *App, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(a, r)
}

func editForm(p Post, action string) url.Values {
	form := url.Values{"title": {p.Title}, "action": {action}}
	for _, it := range p.Items {
		form.Add("file", it.File)
		form.Add("desc", it.Description)
	}
	return form
}

func TestUploadCreatesPostAndFile(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, map[string]string{"title": " Trip "}, upFile{"../../evil.svg", jpegBytes})
	if p.Title != "Trip" || len(p.Items) != 1 {
		t.Fatalf("post: %+v", p)
	}
	name := p.Items[0].File
	if !fileRe.MatchString(name) || !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("file name %q", name)
	}
	if info, _ := os.Stat(filepath.Join(dir, "files", name)); info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestUploadAppendsToExistingPost(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes})
	p2 := mustUpload(t, a, map[string]string{"post": p.Token, "title": "ignored"}, upFile{"b.png", pngBytes})
	if p2.Token != p.Token || len(p2.Items) != 2 || p2.Title != "" || !strings.HasSuffix(p2.Items[1].File, ".png") {
		t.Fatalf("append: %+v", p2)
	}
	before, _ := os.ReadDir(filepath.Join(dir, "files"))
	if w := do(a, uploadReq(map[string]string{"post": newToken()}, upFile{"a.jpg", jpegBytes})); w.Code != http.StatusNotFound {
		t.Fatalf("unknown post: %d", w.Code)
	}
	if after, _ := os.ReadDir(filepath.Join(dir, "files")); len(after) != len(before) {
		t.Fatalf("upload to an unknown post left a file: %d -> %d", len(before), len(after))
	}
}

func TestUploadThatSavedNeverReportsFailure(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes})
	page := filepath.Join(dir, "posts", p.Token, "index.html")
	os.Remove(page)
	os.MkdirAll(filepath.Join(page, "blocker"), 0o755)
	p2 := mustUpload(t, a, map[string]string{"post": p.Token}, upFile{"b.png", pngBytes})
	if len(p2.Items) != 2 {
		t.Fatalf("items: %d", len(p2.Items))
	}
}

func TestUploadRejections(t *testing.T) {
	a, dir := newTestApp(t)
	cases := []struct {
		name string
		req  *http.Request
		code int
	}{
		{"svg", uploadReq(nil, upFile{"x.jpg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")}), http.StatusUnsupportedMediaType},
		{"good then bad", uploadReq(nil, upFile{"a.jpg", jpegBytes}, upFile{"b.html", []byte("<html>")}), http.StatusUnsupportedMediaType},
		{"no files", uploadReq(map[string]string{"title": "x"}), http.StatusBadRequest},
	}
	for _, c := range cases {
		if w := do(a, c.req); w.Code != c.code {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.code)
		}
	}
	for _, sub := range []string{"files", "posts", "tmp"} {
		if left, _ := os.ReadDir(filepath.Join(dir, sub)); len(left) != 0 {
			t.Errorf("%s not empty after rejected uploads: %d entries", sub, len(left))
		}
	}
}

func TestEditSaveMoveRemove(t *testing.T) {
	a, _ := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes}, upFile{"b.png", pngBytes}, upFile{"c.jpg", jpegBytes})
	first, second := p.Items[0].File, p.Items[1].File

	p.Title = "New title"
	p.Items[0].Description = "<b>first</b>"
	if w := postForm(a, "/edit/"+p.Token, editForm(p, "down:0")); w.Code != http.StatusSeeOther {
		t.Fatalf("move: %d %s", w.Code, w.Body)
	}
	p, _ = a.store.Get(p.Token)
	if p.Title != "New title" || p.Items[0].File != second || p.Items[1].File != first || p.Items[1].Description != "<b>first</b>" {
		t.Fatalf("after move: %+v", p)
	}

	if w := postForm(a, "/edit/"+p.Token, editForm(p, "remove:2")); w.Code != http.StatusSeeOther {
		t.Fatalf("remove: %d", w.Code)
	}
	p, _ = a.store.Get(p.Token)
	if len(p.Items) != 2 {
		t.Fatalf("items after remove: %d", len(p.Items))
	}
}

func TestRemoveThatCannotUnlinkChangesNothing(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes}, upFile{"b.png", pngBytes})
	files := filepath.Join(dir, "files")
	os.Chmod(files, 0o555)
	t.Cleanup(func() { os.Chmod(files, 0o755) })
	if w := postForm(a, "/edit/"+p.Token, editForm(p, "remove:1")); w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", w.Code)
	}
	if got, _ := a.store.Get(p.Token); len(got.Items) != 2 {
		t.Fatal("item dropped from the post while its file is still served")
	}
}

func TestRemoveWithFailedPageBuildLeavesNoPublicFile(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes}, upFile{"b.png", pngBytes})
	page := filepath.Join(dir, "posts", p.Token, "index.html")
	os.Remove(page)
	os.MkdirAll(filepath.Join(page, "blocker"), 0o755)
	if w := postForm(a, "/edit/"+p.Token, editForm(p, "remove:1")); w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", p.Items[1].File)); !os.IsNotExist(err) {
		t.Fatal("removed file is still on disk")
	}
}

func TestEditRejectsBadInput(t *testing.T) {
	a, _ := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes})
	other := mustUpload(t, a, nil, upFile{"b.jpg", jpegBytes})

	foreign := editForm(p, "save")
	foreign["file"] = []string{other.Items[0].File}
	cases := []struct {
		name string
		path string
		form url.Values
		code int
	}{
		{"foreign file", "/edit/" + p.Token, foreign, http.StatusConflict},
		{"stale item count", "/edit/" + p.Token, url.Values{"action": {"save"}}, http.StatusConflict},
		{"remove last item", "/edit/" + p.Token, editForm(p, "remove:0"), http.StatusBadRequest},
		{"index out of range", "/edit/" + p.Token, editForm(p, "up:9"), http.StatusBadRequest},
		{"unknown action", "/edit/" + p.Token, editForm(p, "explode:0"), http.StatusBadRequest},
		{"unknown post", "/edit/" + newToken(), editForm(p, "save"), http.StatusNotFound},
	}
	for _, c := range cases {
		if w := postForm(a, c.path, c.form); w.Code != c.code {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.code)
		}
	}
	if w := do(a, httptest.NewRequest("GET", "/edit/not-a-token", nil)); w.Code != http.StatusNotFound {
		t.Errorf("bad token path: %d", w.Code)
	}
}

func TestDeleteNeedsConfirm(t *testing.T) {
	a, _ := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes})
	if w := postForm(a, "/delete/"+p.Token, url.Values{}); w.Code != http.StatusBadRequest {
		t.Fatalf("no confirm: %d", w.Code)
	}
	w := postForm(a, "/delete/"+p.Token, url.Values{"confirm": {"yes"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("delete: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestCrossSitePostsRefused(t *testing.T) {
	a, _ := newTestApp(t)
	cases := []struct {
		name    string
		headers map[string]string
		code    int
	}{
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusSeeOther},
		{"user-initiated", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusSeeOther},
		{"null origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"same-site subdomain", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"origin matches host", map[string]string{"Origin": "https://example.com"}, http.StatusSeeOther},
		{"origin differs", map[string]string{"Origin": "https://evil.test"}, http.StatusForbidden},
	}
	for _, c := range cases {
		r := uploadReq(nil, upFile{"a.jpg", jpegBytes})
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if w := do(a, r); w.Code != c.code {
			t.Errorf("%s: got %d want %d", c.name, w.Code, c.code)
		}
	}
	for _, path := range []string{"/edit/" + newToken(), "/delete/" + newToken()} {
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		if w := do(a, r); w.Code != http.StatusForbidden {
			t.Errorf("%s: got %d want 403", path, w.Code)
		}
	}
}

func TestManageResponsesCarrySecurityHeaders(t *testing.T) {
	a, _ := newTestApp(t)
	for _, path := range []string{"/"} {
		w := do(a, httptest.NewRequest("GET", path, nil))
		h := w.Header()
		csp := h.Get("Content-Security-Policy")
		if w.Code != 200 || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("%s: %d %v", path, w.Code, h)
		}
		if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src") || strings.Contains(csp, "*") {
			t.Errorf("%s: weak policy %q", path, csp)
		}
	}
}

func TestHomeListsEveryPostNewestFirst(t *testing.T) {
	a, _ := newTestApp(t)
	for i := 0; i < 30; i++ {
		file := fmt.Sprintf("%032x.jpg", i)
		a.store.Put(Post{Token: newToken(), Created: time.Unix(int64(i), 0).UTC(), Items: []Item{{File: file}}})
	}
	body := do(a, httptest.NewRequest("GET", "/", nil)).Body.String()
	if got := strings.Count(body, `<article class="card">`); got != 30 {
		t.Fatalf("%d cards, want 30", got)
	}
	all, _ := a.store.List()
	newest := all[0].Token
	if first := strings.Index(body, "/edit/"); body[first+6:first+38] != newest {
		t.Fatal("newest post is not the first card")
	}
}

func TestImageSizesAreStoredAtUploadAndFilledAtStart(t *testing.T) {
	a, dir := newTestApp(t)
	png, err := os.ReadFile(filepath.Join("test", "fixtures", "plain.png"))
	if err != nil {
		t.Fatal(err)
	}
	p := mustUpload(t, a, nil, upFile{"a.png", png}, upFile{"b.jpg", jpegBytes})
	if p.Items[0].Width != 32 || p.Items[0].Height != 24 || p.Items[1].Width != 0 {
		t.Fatalf("sizes at upload: %+v", p.Items)
	}
	p.Items[0].Width, p.Items[0].Height = 0, 0
	a.store.Put(p)
	a.RebuildAll()
	if p, _ = a.store.Get(p.Token); p.Items[0].Width != 32 || p.Items[0].Height != 24 {
		t.Fatalf("sizes after start: %+v", p.Items)
	}
	page, _ := os.ReadFile(filepath.Join(dir, "posts", p.Token, "index.html"))
	if !strings.Contains(string(page), ` width="32" height="24" `) {
		t.Fatal("rebuilt page lacks the image size")
	}
}

func TestRebuildAllWritesEveryPage(t *testing.T) {
	a, dir := newTestApp(t)
	p := mustUpload(t, a, nil, upFile{"a.jpg", jpegBytes})
	page := filepath.Join(dir, "posts", p.Token, "index.html")
	os.Remove(page)
	a.RebuildAll()
	if _, err := os.Stat(page); err != nil {
		t.Fatal("page not rebuilt")
	}
}

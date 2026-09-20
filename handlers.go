package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxUpload   = 100 << 20
	manageCSP   = "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	maxFormSize = 1 << 20
)

var errUnsupported = errors.New("unsupported file type; accepted: JPEG, PNG, GIF, WebP, AVIF, MP4, WebM")

type App struct {
	store   *Store
	baseURL string
	mu      sync.Mutex
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("POST /upload", a.upload)
	mux.HandleFunc("GET /edit/{token}", a.editPage)
	mux.HandleFunc("POST /edit/{token}", a.editSave)
	mux.HandleFunc("POST /delete/{token}", a.deletePost)
	return guard(http.NewCrossOriginProtection().Handler(mux))
}

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "private, no-store, no-transform")
		h.Set("Content-Security-Policy", manageCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		// no-referrer would make browsers send "Origin: null" on form posts, which the cross-origin check refuses.
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (a *App) save(p Post) error {
	if err := a.store.Put(p); err != nil {
		return err
	}
	return a.BuildPage(p)
}

func (a *App) RebuildAll() (int, error) {
	posts, err := a.store.List()
	if err != nil {
		return 0, err
	}
	for _, p := range posts {
		if a.fillSizes(&p) {
			if err := a.store.Put(p); err != nil {
				log.Printf("save %s: %v", p.Token, err)
			}
		}
		if err := a.BuildPage(p); err != nil {
			log.Printf("rebuild %s: %v", p.Token, err)
		}
	}
	return len(posts), nil
}

func (a *App) fillSizes(p *Post) bool {
	changed := false
	for i, it := range p.Items {
		if it.Width > 0 || isVideo(it.File) {
			continue
		}
		if w, h, animated := imageSize(a.store.FilePath(it.File)); w > 0 && h > 0 {
			p.Items[i].Width, p.Items[i].Height, p.Items[i].Animated = w, h, animated
			changed = true
		}
	}
	return changed
}

func render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

type cardView struct {
	Token, Title, Date, Preview, Link string
	Count                             int
	Video                             bool
}

type homeView struct {
	Posts []cardView
	Total int
}

func (a *App) card(p Post) cardView {
	c := cardView{Token: p.Token, Title: p.Title, Date: p.Created.Format("2006-01-02"), Link: a.baseURL + "/a/" + p.Token, Count: len(p.Items)}
	if len(p.Items) > 0 {
		c.Preview = p.Items[0].File
		c.Video = isVideo(c.Preview)
	}
	return c
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	all, err := a.store.List()
	a.mu.Unlock()
	if err != nil {
		log.Printf("list: %v", err)
		http.Error(w, "could not read the posts", http.StatusInternalServerError)
		return
	}
	v := homeView{Total: len(all)}
	for _, p := range all {
		v.Posts = append(v.Posts, a.card(p))
	}
	render(w, "home.html", v)
}

func readField(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, maxFormSize))
	return strings.TrimSpace(string(b))
}

func (a *App) storeFile(src io.Reader) (Item, error) {
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(src, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Item{}, err
	}
	head = head[:n]
	ext, ok := sniff(head)
	if !ok {
		return Item{}, errUnsupported
	}
	f, name, err := a.store.NewFile(ext)
	if err != nil {
		return Item{}, err
	}
	_, err = f.Write(head)
	if err == nil {
		_, err = io.Copy(f, src)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		a.store.RemoveFile(name)
		return Item{}, err
	}
	w, h, animated := imageSize(a.store.FilePath(name))
	return Item{File: name, Width: w, Height: h, Animated: animated}, nil
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected a multipart form", http.StatusBadRequest)
		return
	}
	var title, target string
	var items []Item
	fail := func(status int, msg string) {
		for _, it := range items {
			a.store.RemoveFile(it.File)
		}
		http.Error(w, msg, status)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err == nil {
			switch {
			case part.FormName() == "title":
				title = readField(part)
			case part.FormName() == "post":
				target = readField(part)
			case part.FormName() == "files" && part.FileName() != "":
				var it Item
				if it, err = a.storeFile(part); err == nil {
					items = append(items, it)
				}
			}
		}
		var tooBig *http.MaxBytesError
		switch {
		case err == nil:
		case errors.As(err, &tooBig):
			fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("upload is over the %d MiB limit", maxUpload>>20))
			return
		case errors.Is(err, errUnsupported):
			fail(http.StatusUnsupportedMediaType, err.Error())
			return
		default:
			log.Printf("upload: %v", err)
			fail(http.StatusBadRequest, "upload failed")
			return
		}
	}
	if len(items) == 0 {
		fail(http.StatusBadRequest, "no files sent")
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	p := Post{Token: newToken(), Title: title, Created: time.Now().UTC()}
	if target != "" {
		var err error
		if p, err = a.store.Get(target); err != nil {
			fail(http.StatusNotFound, "post not found")
			return
		}
	}
	p.Items = append(p.Items, items...)
	if err := a.store.Put(p); err != nil {
		log.Printf("save %s: %v", p.Token, err)
		fail(http.StatusInternalServerError, "could not save the post")
		return
	}
	if err := a.BuildPage(p); err != nil {
		log.Printf("build %s: %v", p.Token, err)
	}
	http.Redirect(w, r, "/edit/"+p.Token, http.StatusSeeOther)
}

type editItem struct {
	Index              int
	Num, File, Link    string
	Description        string
	Video, First, Last bool
}

type editView struct {
	Token, Title, Link string
	Album              bool
	Items              []editItem
}

func (a *App) editPage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	p, err := a.store.Get(r.PathValue("token"))
	a.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	v := editView{Token: p.Token, Title: p.Title, Link: a.baseURL + "/a/" + p.Token, Album: len(p.Items) > 1}
	for i, it := range p.Items {
		v.Items = append(v.Items, editItem{
			Index: i, Num: fmt.Sprintf("%02d", i+1), File: it.File, Link: a.baseURL + "/i/" + it.File,
			Description: it.Description, Video: isVideo(it.File), First: i == 0, Last: i == len(p.Items)-1,
		})
	}
	render(w, "edit.html", v)
}

func (a *App) editSave(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.store.Get(r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	files, descs := r.PostForm["file"], r.PostForm["desc"]
	have := map[string]Item{}
	for _, it := range p.Items {
		have[it.File] = it
	}
	if len(files) != len(p.Items) || len(descs) != len(files) {
		http.Error(w, "the post changed in another tab; reload and try again", http.StatusConflict)
		return
	}
	items := make([]Item, 0, len(files))
	for i, f := range files {
		it, ok := have[f]
		if !ok {
			http.Error(w, "the post changed in another tab; reload and try again", http.StatusConflict)
			return
		}
		delete(have, f)
		it.Description = strings.TrimSpace(descs[i])
		items = append(items, it)
	}
	p.Title = strings.TrimSpace(r.PostForm.Get("title"))

	verb, arg, _ := strings.Cut(r.PostForm.Get("action"), ":")
	i, err := strconv.Atoi(arg)
	if verb != "save" && (err != nil || i < 0 || i >= len(items)) {
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	var removed string
	switch verb {
	case "save":
	case "up":
		if i > 0 {
			items[i-1], items[i] = items[i], items[i-1]
		}
	case "down":
		if i < len(items)-1 {
			items[i+1], items[i] = items[i], items[i+1]
		}
	case "remove":
		if len(items) < 2 {
			http.Error(w, "delete the post to remove its last item", http.StatusBadRequest)
			return
		}
		removed = items[i].File
		items = append(items[:i], items[i+1:]...)
	default:
		http.Error(w, "bad action", http.StatusBadRequest)
		return
	}
	if removed != "" {
		if err := a.store.RemoveFile(removed); err != nil {
			log.Printf("remove %s: %v", removed, err)
			http.Error(w, "could not remove the file", http.StatusInternalServerError)
			return
		}
	}
	p.Items = items
	if err := a.save(p); err != nil {
		log.Printf("save %s: %v", p.Token, err)
		http.Error(w, "could not save the post", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/edit/"+p.Token, http.StatusSeeOther)
}

func (a *App) deletePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if r.FormValue("confirm") != "yes" {
		http.Error(w, "tick the confirm box to delete", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	token := r.PathValue("token")
	err := a.store.Delete(token)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("delete %s: %v", token, err)
		http.Error(w, "could not delete the post", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

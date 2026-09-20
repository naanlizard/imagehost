package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var (
	tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	fileRe  = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png|gif|webp|avif|mp4|webm)$`)
)

type Item struct {
	File        string `json:"file"`
	Description string `json:"description"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Animated    bool   `json:"animated,omitempty"`
}

type Post struct {
	Token   string    `json:"token"`
	Title   string    `json:"title"`
	Created time.Time `json:"created"`
	Items   []Item    `json:"items"`
}

type Store struct {
	dir string
}

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func OpenStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := os.RemoveAll(s.path("tmp")); err != nil {
		return nil, err
	}
	for _, d := range []string{"files", "posts", "tmp"} {
		if err := os.MkdirAll(s.path(d), 0o755); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

func (s *Store) PagePath(token string) string { return s.path("posts", token, "index.html") }

func (s *Store) FilePath(name string) string { return s.path("files", name) }

func (s *Store) Get(token string) (Post, error) {
	var p Post
	if !tokenRe.MatchString(token) {
		return p, os.ErrNotExist
	}
	data, err := os.ReadFile(s.path("posts", token, "post.json"))
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("%s: %w", token, err)
	}
	if p.Token != token {
		return p, fmt.Errorf("%s: token mismatch", token)
	}
	return p, nil
}

func (s *Store) List() ([]Post, error) {
	entries, err := os.ReadDir(s.path("posts"))
	if err != nil {
		return nil, err
	}
	var out []Post
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := s.Get(e.Name())
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].Token < out[j].Token
	})
	return out, nil
}

func (s *Store) Put(p Post) error {
	if !tokenRe.MatchString(p.Token) {
		return errors.New("bad token")
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.path("posts", p.Token), 0o755); err != nil {
		return err
	}
	return s.WriteAtomic(s.path("posts", p.Token, "post.json"), data)
}

func (s *Store) Delete(token string) error {
	p, err := s.Get(token)
	if err != nil {
		return err
	}
	for _, it := range p.Items {
		if err := s.RemoveFile(it.File); err != nil {
			return err
		}
	}
	return os.RemoveAll(s.path("posts", token))
}

func (s *Store) RemoveFile(name string) error {
	if !fileRe.MatchString(name) {
		return errors.New("bad file name")
	}
	err := os.Remove(s.path("files", name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) NewFile(ext string) (*os.File, string, error) {
	name := newToken() + "." + ext
	if !fileRe.MatchString(name) {
		return nil, "", errors.New("bad file type")
	}
	f, err := os.OpenFile(s.path("files", name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	return f, name, err
}

func (s *Store) WriteAtomic(dst string, data []byte) error {
	tmp := s.path("tmp", newToken())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

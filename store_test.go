package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func addFile(t *testing.T, s *Store, ext string) string {
	t.Helper()
	f, name, err := s.NewFile(ext)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("data")
	f.Close()
	return name
}

func TestStoreRoundTripAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	file := addFile(t, s, "jpg")
	old := Post{Token: newToken(), Title: "old", Created: time.Unix(100, 0).UTC(), Items: []Item{{File: file, Description: "d"}}}
	recent := Post{Token: newToken(), Title: "new", Created: time.Unix(200, 0).UTC(), Items: []Item{{File: addFile(t, s, "png")}}}
	for _, p := range []Post{old, recent} {
		if err := s.Put(p); err != nil {
			t.Fatal(err)
		}
	}

	s2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(old.Token)
	if err != nil || got.Title != "old" || len(got.Items) != 1 || got.Items[0] != old.Items[0] || !got.Created.Equal(old.Created) {
		t.Fatalf("reload mismatch: %+v", got)
	}
	list, err := s2.List()
	if err != nil || len(list) != 2 || list[0].Token != recent.Token {
		t.Fatalf("list order: %+v", list)
	}
	left, _ := os.ReadDir(filepath.Join(dir, "tmp"))
	if len(left) != 0 {
		t.Fatalf("tmp not empty: %d", len(left))
	}
}

func TestOpenStoreClearsTmpAndListSkipsEmptyPostDirs(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "posts", newToken())
	os.MkdirAll(empty, 0o755)
	os.WriteFile(filepath.Join(dir, "posts", newToken()), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "tmp"), 0o755)
	os.WriteFile(filepath.Join(dir, "tmp", "leftover"), []byte("x"), 0o644)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty post dir and stray file: %d posts, %v", len(list), err)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(left) != 0 {
		t.Fatal("tmp not cleared at open")
	}
}

func TestListRejectsCorruptOrMismatchedPosts(t *testing.T) {
	for name, body := range map[string]string{"corrupt": "{", "mismatch": `{"token":"` + newToken() + `"}`} {
		dir := t.TempDir()
		post := filepath.Join(dir, "posts", newToken())
		os.MkdirAll(post, 0o755)
		os.WriteFile(filepath.Join(post, "post.json"), []byte(body), 0o644)
		s, _ := OpenStore(dir)
		if _, err := s.List(); err == nil {
			t.Errorf("%s post.json accepted", name)
		}
	}
}

func TestStoreDeleteRemovesFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenStore(dir)
	file := addFile(t, s, "gif")
	p := Post{Token: newToken(), Created: time.Now().UTC(), Items: []Item{{File: file}}}
	s.Put(p)
	if err := s.Delete(p.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", file)); !os.IsNotExist(err) {
		t.Fatal("file still present")
	}
	if _, err := os.Stat(filepath.Join(dir, "posts", p.Token)); !os.IsNotExist(err) {
		t.Fatal("post dir still present")
	}
	if _, err := s.Get(p.Token); err == nil {
		t.Fatal("post still listed")
	}
}

func TestStoreRejectsBadNames(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	if err := s.Put(Post{Token: "../x"}); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := s.RemoveFile("../../etc/passwd"); err == nil {
		t.Fatal("bad file accepted")
	}
	if _, _, err := s.NewFile("svg"); err == nil {
		t.Fatal("bad file type accepted")
	}
}

func TestTokenShape(t *testing.T) {
	a, b := newToken(), newToken()
	if !tokenRe.MatchString(a) || a == b {
		t.Fatalf("tokens: %q %q", a, b)
	}
}

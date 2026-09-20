package main

import "testing"

func ftyp(major string, compat ...string) []byte {
	size := 16 + 4*len(compat)
	b := []byte{0, 0, 0, byte(size)}
	b = append(b, "ftyp"...)
	b = append(b, major...)
	b = append(b, 0, 0, 0, 0)
	for _, c := range compat {
		b = append(b, c...)
	}
	return append(b, make([]byte, 32)...)
}

func TestSniff(t *testing.T) {
	ebml := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x82, 0x84}
	cases := []struct {
		name string
		head []byte
		ext  string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "jpg"},
		{"png", []byte("\x89PNG\r\n\x1a\n...."), "png"},
		{"gif87", []byte("GIF87a...."), "gif"},
		{"gif89", []byte("GIF89a...."), "gif"},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "webp"},
		{"riff not webp", []byte("RIFF\x00\x00\x00\x00WAVEfmt "), ""},
		{"webm", append(append([]byte{}, ebml...), "webm"...), "webm"},
		{"mkv", append(append([]byte{}, ebml...), "matroska"...), ""},
		{"avif", ftyp("avif", "mif1", "miaf"), "avif"},
		{"avif by compat", ftyp("mif1", "avif", "miaf"), "avif"},
		{"heic", ftyp("heic", "mif1"), ""},
		{"mif1 alone", ftyp("mif1", "miaf"), ""},
		{"mp4", ftyp("isom", "iso2", "avc1", "mp41"), "mp4"},
		{"mp42", ftyp("mp42", "isom"), "mp4"},
		{"unknown brand", ftyp("qt  ", "qt  "), ""},
		{"svg", []byte("<svg xmlns="), ""},
		{"html", []byte("<!doctype html>"), ""},
		{"empty", nil, ""},
		{"short ftyp", []byte("\x00\x00\x00\x10ftyp"), ""},
	}
	for _, c := range cases {
		ext, ok := sniff(c.head)
		if ext != c.ext || ok != (c.ext != "") {
			t.Errorf("%s: got %q %v, want %q", c.name, ext, ok, c.ext)
		}
	}
}

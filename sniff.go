package main

import (
	"bytes"
	"encoding/binary"
)

const sniffLen = 4096

var (
	avifBrands = []string{"avif", "avis"}
	mp4Brands  = []string{"isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "M4V ", "dash"}
)

func sniff(head []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return "jpg", true
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "png", true
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "gif", true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return "webp", true
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		if bytes.Contains(head[:min(len(head), 64)], []byte("webm")) {
			return "webm", true
		}
	case len(head) >= 16 && string(head[4:8]) == "ftyp":
		return sniffBMFF(head)
	}
	return "", false
}

func sniffBMFF(head []byte) (string, bool) {
	size := int(binary.BigEndian.Uint32(head[:4]))
	if size < 16 || size > len(head) {
		return "", false
	}
	brands := []string{string(head[8:12])}
	for off := 16; off+4 <= size; off += 4 {
		brands = append(brands, string(head[off:off+4]))
	}
	for _, set := range []struct {
		ext    string
		brands []string
	}{{"avif", avifBrands}, {"mp4", mp4Brands}} {
		for _, b := range brands {
			for _, want := range set.brands {
				if b == want {
					return set.ext, true
				}
			}
		}
	}
	return "", false
}

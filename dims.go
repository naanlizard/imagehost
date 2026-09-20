package main

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
)

const dimsHead = 1 << 16

// Dimensions as a browser lays the image out, so a JPEG turned on its side by its EXIF tag is swapped.
func imageSize(path string) (w, h int, animated bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	head := make([]byte, dimsHead)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch filepath.Ext(path) {
	case ".jpg", ".png", ".gif":
		cfg, _, err := image.DecodeConfig(io.MultiReader(bytes.NewReader(head), f))
		if err != nil {
			return
		}
		w, h, animated = cfg.Width, cfg.Height, filepath.Ext(path) == ".gif"
		if filepath.Ext(path) == ".jpg" && jpegOrientation(head) >= 5 {
			w, h = h, w
		}
	case ".webp":
		w, h = webpSize(head)
		animated = len(head) > 20 && string(head[12:16]) == "VP8X" && head[20]&0x02 != 0
	case ".avif":
		w, h = avifSize(head)
	}
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return
}

func jpegOrientation(b []byte) int {
	i := bytes.Index(b, []byte("Exif\x00\x00"))
	if i < 0 || len(b) < i+14 {
		return 1
	}
	t := b[i+6:]
	var bo binary.ByteOrder = binary.BigEndian
	if string(t[:2]) == "II" {
		bo = binary.LittleEndian
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	for k, n := 0, int(bo.Uint16(t[off:])); k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			return int(bo.Uint16(t[e+8:]))
		}
	}
	return 1
}

func webpSize(b []byte) (int, int) {
	if len(b) < 30 {
		return 0, 0
	}
	u24 := func(p []byte) int { return int(p[0]) | int(p[1])<<8 | int(p[2])<<16 }
	switch string(b[12:16]) {
	case "VP8X":
		return u24(b[24:]) + 1, u24(b[27:]) + 1
	case "VP8L":
		v := binary.LittleEndian.Uint32(b[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1
	case "VP8 ":
		return int(binary.LittleEndian.Uint16(b[26:]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:]) & 0x3fff)
	}
	return 0, 0
}

// An AVIF built from tiles lists each tile's size as well; the whole image is the largest.
func avifSize(b []byte) (w, h int) {
	for {
		i := bytes.Index(b, []byte("ispe"))
		if i < 0 || len(b) < i+16 {
			return w, h
		}
		iw, ih := int(binary.BigEndian.Uint32(b[i+8:])), int(binary.BigEndian.Uint32(b[i+12:]))
		if iw <= 1<<16 && ih <= 1<<16 && iw*ih > w*h {
			w, h = iw, ih
		}
		b = b[i+16:]
	}
}

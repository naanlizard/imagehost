package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestImageSizeOnFixtures(t *testing.T) {
	for _, name := range []string{"plain.jpg", "plain.png", "plain.gif", "plain.webp"} {
		if w, h, animated := imageSize(filepath.Join("test", "fixtures", name)); w != 32 || h != 24 || animated != (name == "plain.gif") {
			t.Errorf("%s: %dx%d animated=%v, want 32x24", name, w, h, animated)
		}
	}
	for _, name := range []string{"clip.mp4", "missing.png"} {
		if w, h, _ := imageSize(filepath.Join("test", "fixtures", name)); w != 0 || h != 0 {
			t.Errorf("%s: %dx%d, want none", name, w, h)
		}
	}
}

func exifOrientation(o uint16) []byte {
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00")
	tiff = binary.LittleEndian.AppendUint16(tiff, 0x0112)
	tiff = append(tiff, 3, 0, 1, 0, 0, 0)
	tiff = binary.LittleEndian.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	return append(out, seg...)
}

func TestImageSizeFollowsJPEGOrientation(t *testing.T) {
	var enc bytes.Buffer
	jpeg.Encode(&enc, image.NewRGBA(image.Rect(0, 0, 40, 10)), nil)
	for o, want := range map[uint16][2]int{1: {40, 10}, 3: {40, 10}, 6: {10, 40}, 8: {10, 40}} {
		data := append(append([]byte{0xFF, 0xD8}, exifOrientation(o)...), enc.Bytes()[2:]...)
		path := filepath.Join(t.TempDir(), "x.jpg")
		os.WriteFile(path, data, 0o644)
		if w, h, _ := imageSize(path); w != want[0] || h != want[1] {
			t.Errorf("orientation %d: %dx%d, want %dx%d", o, w, h, want[0], want[1])
		}
	}
	if o := jpegOrientation([]byte("Exif\x00\x00II*\x00\xff\xff\xff\xff")); o != 1 {
		t.Errorf("bad offset: %d", o)
	}
}

func TestWebPAndAVIFSizes(t *testing.T) {
	riff := func(kind string, payload []byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"), kind...)
		return append(append(b, 0, 0, 0, 0), append(payload, make([]byte, 16)...)...)
	}
	vp8x := []byte{0, 0, 0, 0, 0xCF, 0x07, 0, 0xAF, 0x04, 0}
	vp8l := binary.LittleEndian.AppendUint32([]byte{0x2f}, 1999|1199<<14)
	for name, b := range map[string][]byte{"VP8X": riff("VP8X", vp8x), "VP8L": riff("VP8L", vp8l)} {
		if w, h := webpSize(b); w != 2000 || h != 1200 {
			t.Errorf("%s: %dx%d", name, w, h)
		}
	}
	ispe := func(w, h uint32) []byte {
		b := append([]byte("ispe"), 0, 0, 0, 0)
		return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(b, w), h)
	}
	avif := append(append([]byte("\x00\x00\x00\x18ftypavif"), ispe(512, 512)...), ispe(2000, 1200)...)
	avif = append(avif, ispe(0xFFFFFFFF, 0xFFFFFFFF)...)
	if w, h := avifSize(avif); w != 2000 || h != 1200 {
		t.Errorf("avif: %dx%d", w, h)
	}
	if w, h := webpSize([]byte("RIFF")); w != 0 || h != 0 {
		t.Errorf("short webp: %dx%d", w, h)
	}
	flat := filepath.Join(t.TempDir(), "flat.webp")
	os.WriteFile(flat, riff("VP8 ", []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 0, 0, 0xAF, 0x04}), 0o644)
	if w, h, _ := imageSize(flat); w != 0 || h != 0 {
		t.Errorf("webp with a zero width: %dx%d, want none", w, h)
	}
	moving := filepath.Join(t.TempDir(), "x.webp")
	os.WriteFile(moving, riff("VP8X", append([]byte{0x02}, vp8x[1:]...)), 0o644)
	if w, h, animated := imageSize(moving); w != 2000 || h != 1200 || !animated {
		t.Errorf("animated webp: %dx%d animated=%v", w, h, animated)
	}
}

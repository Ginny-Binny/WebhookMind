package extraction

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		want   string
	}{
		{"pdf magic", []byte("%PDF-1.4\n%..."), "pdf"},
		{"jpeg magic", []byte{0xFF, 0xD8, 0xFF, 0xE0, 'J', 'F', 'I', 'F'}, "image"},
		{"png magic", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image"},
		{"tiff little-endian", []byte{0x49, 0x49, 0x2A, 0x00, 0, 0, 0, 0}, "image"},
		{"tiff big-endian", []byte{0x4D, 0x4D, 0x00, 0x2A, 0, 0, 0, 0}, "image"},
		{"mp3 with ID3 tag", []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00"), "audio"},
		{"mp3 sync word FF FB", []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}, "audio"},
		{"wav RIFF/WAVE", []byte("RIFF\x00\x00\x00\x00WAVE"), "audio"},
		{"m4a ftyp box", []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}, "audio"},
		{"xml leading angle bracket", []byte("<?xml version=\"1.0\"?>"), "xml"},
		{"xml with leading whitespace", []byte("   <?xml ?><x/>"), "xml"},
		{"xml with UTF-8 BOM", []byte{0xEF, 0xBB, 0xBF, '<', '?', 'x', 'm', 'l', '?', '>', '<', '/'}, "xml"},
		{"csv with comma", []byte("a,b,c\n1,2,3\n"), "csv"},
		{"too short", []byte("ab"), "unknown"},
		{"random binary", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, "unknown"},
		{"plain ASCII no comma", []byte("hello world"), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectFileType(tc.header)
			assert.Equal(t, tc.want, got)
		})
	}
}

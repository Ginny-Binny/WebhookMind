package extraction

import "bytes"

// DetectFileType detects file type from the first 16 bytes using magic bytes.
func DetectFileType(header []byte) string {
	if len(header) < 4 {
		return "unknown"
	}

	// PDF: starts with %PDF
	if bytes.HasPrefix(header, []byte("%PDF")) {
		return "pdf"
	}

	// JPEG: FF D8 FF
	if len(header) >= 3 && header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF {
		return "image"
	}

	// PNG: 89 50 4E 47 (‰PNG)
	if bytes.HasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return "image"
	}

	// TIFF: Little-endian (49 49 2A 00) or Big-endian (4D 4D 00 2A)
	if len(header) >= 4 {
		if header[0] == 0x49 && header[1] == 0x49 && header[2] == 0x2A && header[3] == 0x00 {
			return "image"
		}
		if header[0] == 0x4D && header[1] == 0x4D && header[2] == 0x00 && header[3] == 0x2A {
			return "image"
		}
	}

	// MP3: ID3 header
	if bytes.HasPrefix(header, []byte("ID3")) {
		return "audio"
	}

	// MP3: sync word FF FB / FF FA / FF F3 / FF F2
	if len(header) >= 2 && header[0] == 0xFF && (header[1]&0xE0) == 0xE0 {
		return "audio"
	}

	// WAV: RIFF....WAVE
	if len(header) >= 12 && bytes.HasPrefix(header, []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WAVE")) {
		return "audio"
	}

	// M4A/MP4: ftyp box
	if len(header) >= 8 && bytes.Equal(header[4:8], []byte("ftyp")) {
		return "audio"
	}

	// Try text-based formats: CSV and XML
	// Skip leading whitespace/BOM
	text := header
	if len(text) >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF {
		text = text[3:] // skip UTF-8 BOM
	}
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t' || text[0] == '\n' || text[0] == '\r') {
		text = text[1:]
	}

	if len(text) > 0 && text[0] == '<' {
		return "xml"
	}

	// Simple CSV heuristic: printable ASCII with commas
	if isPrintableASCII(header) && bytes.Contains(header, []byte(",")) {
		return "csv"
	}

	return "unknown"
}

func isPrintableASCII(data []byte) bool {
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}

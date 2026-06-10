package extraction

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildFakeDocx constructs a minimal in-memory .docx archive containing the given
// paragraphs. Each paragraph becomes one `<w:p>` with a single `<w:t>` text run.
func buildFakeDocx(t *testing.T, paragraphs []string) []byte {
	t.Helper()

	var xmlBuf bytes.Buffer
	xmlBuf.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	xmlBuf.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		xmlBuf.WriteString(`<w:p><w:r><w:t>`)
		xmlBuf.WriteString(p)
		xmlBuf.WriteString(`</w:t></w:r></w:p>`)
	}
	xmlBuf.WriteString(`</w:body></w:document>`)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("word/document.xml")
	require.NoError(t, err)
	_, err = f.Write(xmlBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	return zipBuf.Bytes()
}

func TestExtractDocxText_SinglePara(t *testing.T) {
	docx := buildFakeDocx(t, []string{"Hello, world."})
	text, err := ExtractDocxText(docx)
	require.NoError(t, err)
	assert.Equal(t, "Hello, world.", text)
}

func TestExtractDocxText_MultiPara(t *testing.T) {
	docx := buildFakeDocx(t, []string{"Invoice #42", "Total: $100.00", "Date: 2026-05-20"})
	text, err := ExtractDocxText(docx)
	require.NoError(t, err)
	assert.Equal(t, "Invoice #42\nTotal: $100.00\nDate: 2026-05-20", text)
}

func TestExtractDocxText_NotADocx(t *testing.T) {
	// Valid zip but missing word/document.xml.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, _ := zw.Create("xl/workbook.xml")
	_, _ = f.Write([]byte("<excel/>"))
	_ = zw.Close()

	_, err := ExtractDocxText(zipBuf.Bytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "word/document.xml")
}

func TestExtractDocxText_NotAZip(t *testing.T) {
	_, err := ExtractDocxText([]byte("this is not a zip file"))
	require.Error(t, err)
}

func TestIsDocxZip(t *testing.T) {
	docx := buildFakeDocx(t, []string{"x"})
	assert.True(t, IsDocxZip(docx))
	assert.False(t, IsDocxZip([]byte("nope")))
}

func TestClassifyZip(t *testing.T) {
	docx := buildFakeDocx(t, []string{"x"})
	assert.Equal(t, "docx", ClassifyZip(docx))

	// Valid zip but not a docx → "unknown".
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, _ := zw.Create("xl/workbook.xml")
	_, _ = f.Write([]byte("<excel/>"))
	_ = zw.Close()
	assert.Equal(t, "unknown", ClassifyZip(zipBuf.Bytes()))
}

func TestDetectFileType_Zip(t *testing.T) {
	// First 4 bytes of any ZIP file are PK\x03\x04.
	header := []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00}
	assert.Equal(t, "zip", DetectFileType(header))
}

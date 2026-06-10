package extraction

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ExtractDocxText pulls the plain text out of a .docx file (Office Open XML).
//
// A DOCX file is a ZIP archive; the document body lives in `word/document.xml`,
// where each text run is a `<w:t>` element and paragraphs are `<w:p>` elements.
// We stream-decode that XML, collect text nodes, and emit a paragraph break
// between adjacent `<w:p>` blocks. Tables and inline images are dropped.
//
// This exists because neither Anthropic nor OpenAI accept raw .docx binaries
// as input — both LLMs handle the extracted plain text just fine for structured
// field extraction.
func ExtractDocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}

	var documentXML *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			documentXML = f
			break
		}
	}
	if documentXML == nil {
		return "", errors.New("not a valid docx: word/document.xml not found")
	}

	rc, err := documentXML.Open()
	if err != nil {
		return "", fmt.Errorf("open document.xml: %w", err)
	}
	defer rc.Close()

	return parseDocumentXML(rc)
}

// IsDocxZip checks whether the given ZIP bytes look like a DOCX (i.e. they contain
// `word/document.xml`). Used by the bridge to distinguish DOCX from other ZIP-based
// formats (XLSX/PPTX/JAR/...) without trying to fully parse them.
func IsDocxZip(data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			return true
		}
	}
	return false
}

// ClassifyZip determines the specific file format of a ZIP-based document.
// DetectFileType returns "zip" on the magic bytes alone; this function inspects
// the central directory to refine the classification. Returns "docx" when a
// `word/document.xml` entry is present, "unknown" otherwise.
func ClassifyZip(data []byte) string {
	if IsDocxZip(data) {
		return "docx"
	}
	return "unknown"
}

func parseDocumentXML(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)

	var out strings.Builder
	var paragraph strings.Builder
	inText := false

	flushParagraph := func() {
		text := strings.TrimRight(paragraph.String(), " \t")
		if text != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(text)
		}
		paragraph.Reset()
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("parse document.xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				paragraph.WriteString("\t")
			case "br":
				paragraph.WriteString("\n")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				flushParagraph()
			}
		case xml.CharData:
			if inText {
				paragraph.Write(t)
			}
		}
	}

	// Any trailing text not closed by </w:p>.
	flushParagraph()

	return out.String(), nil
}

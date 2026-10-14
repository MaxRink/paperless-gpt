package ocr

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTesseractProviderParsesBoundedLocalHOCR(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tesseract")
	script := `#!/bin/sh
cat <<'HTML'
<html><body><div class='ocr_page' id='page_1' title='bbox 0 0 1000 1400; ppageno 0'><span class='ocr_line' id='line_1' title='bbox 100 200 500 260'><span class='ocrx_word' id='word_1' title='bbox 100 200 300 260; x_wconf 96'>AT&amp;amp;T</span> <span class='ocrx_word' id='word_2' title='bbox 320 200 500 260; x_wconf 91'>123</span></span></div></body></html>
HTML`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0700))
	provider, err := newTesseractProvider(Config{TesseractBinary: bin, TesseractLanguages: "deu+eng", TesseractTimeoutSeconds: 5, TesseractPSM: 3})
	require.NoError(t, err)
	result, err := provider.ProcessImage(context.Background(), []byte("synthetic image"), 1)
	require.NoError(t, err)
	require.Equal(t, "AT&amp;T 123", result.Text)
	require.NotNil(t, result.HOCRPage)
	require.Len(t, result.HOCRPage.Lines, 1)
	require.Len(t, result.HOCRPage.Lines[0].Words, 2)
	require.Equal(t, "AT&amp;T", result.HOCRPage.Lines[0].Words[0].Text)
	require.Equal(t, float64(96), result.HOCRPage.Lines[0].Words[0].Confidence)
	require.Equal(t, 1000.0, result.HOCRPage.BBox.X2)
}

func TestTesseractProviderRejectsUnsafeLanguageAndBounds(t *testing.T) {
	_, err := newTesseractProvider(Config{TesseractLanguages: "deu;cat /tmp/x"})
	require.ErrorContains(t, err, "invalid TESSERACT_LANGUAGES")
	_, err = newTesseractProvider(Config{TesseractLanguages: "deu", TesseractTimeoutSeconds: 601})
	require.ErrorContains(t, err, "invalid TESSERACT_TIMEOUT_SECONDS")
}

func TestParseTesseractHOCRRejectsMissingPageGeometryAndAllowsBlankPage(t *testing.T) {
	_, _, err := parseTesseractHOCR([]byte(`<div class="ocr_page" title="ppageno 0"><span class="ocr_line"><span class="ocrx_word">x</span></span></div>`), 1)
	require.ErrorContains(t, err, "valid bounding box")
	page, text, err := parseTesseractHOCR([]byte(`<div class="ocr_page" title="bbox 0 0 100 100; ppageno 0"></div>`), 1)
	require.NoError(t, err)
	require.Empty(t, text)
	require.Empty(t, page.Lines)
}

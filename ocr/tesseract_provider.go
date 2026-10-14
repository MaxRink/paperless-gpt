package ocr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gardar/ocrchestra/pkg/hocr"
)

const (
	defaultTesseractBinary    = "tesseract"
	defaultTesseractLanguages = "deu+eng"
	defaultTesseractTimeout   = 60 * time.Second
	defaultTesseractMaxOutput = 8 << 20
	defaultTesseractPSM       = 3
)

// TesseractProvider runs the locally installed Tesseract binary and converts
// its hOCR output into the provider-neutral OCR/hOCR structures. It is kept
// deliberately local: no document bytes leave the process.
type TesseractProvider struct {
	binary    string
	languages string
	timeout   time.Duration
	psm       int
	maxOutput int64

	mu        sync.Mutex
	hocrPages []hocr.Page
}

func newTesseractProvider(config Config) (*TesseractProvider, error) {
	binary := strings.TrimSpace(config.TesseractBinary)
	if binary == "" {
		binary = defaultTesseractBinary
	}
	languages := strings.TrimSpace(config.TesseractLanguages)
	if languages == "" {
		languages = defaultTesseractLanguages
	}
	if !validTesseractLanguages(languages) {
		return nil, fmt.Errorf("invalid TESSERACT_LANGUAGES: only language codes joined by '+' are allowed")
	}
	psm := config.TesseractPSM
	if psm == 0 {
		psm = defaultTesseractPSM
	}
	if psm < 3 || psm > 13 {
		return nil, fmt.Errorf("invalid TESSERACT_PSM %d: expected 3..13", psm)
	}
	timeout := time.Duration(config.TesseractTimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTesseractTimeout
	}
	if timeout < time.Second || timeout > 10*time.Minute {
		return nil, fmt.Errorf("invalid TESSERACT_TIMEOUT_SECONDS: expected 1..600")
	}
	maxOutput := int64(config.TesseractMaxOutputBytes)
	if maxOutput == 0 {
		maxOutput = defaultTesseractMaxOutput
	}
	if maxOutput < 4096 || maxOutput > 64<<20 {
		return nil, fmt.Errorf("invalid TESSERACT_MAX_OUTPUT_BYTES: expected 4096..67108864")
	}
	return &TesseractProvider{binary: binary, languages: languages, timeout: timeout, psm: psm, maxOutput: maxOutput}, nil
}

func validTesseractLanguages(value string) bool {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasSuffix(value, "+") {
		return false
	}
	for _, part := range strings.Split(value, "+") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.Len()+len(p)) > b.limit {
		return 0, fmt.Errorf("tesseract output exceeded %d bytes", b.limit)
	}
	return b.Buffer.Write(p)
}

func (p *TesseractProvider) ProcessImage(parent context.Context, imageContent []byte, pageNumber int) (*OCRResult, error) {
	if len(imageContent) == 0 {
		return nil, fmt.Errorf("cannot OCR an empty image")
	}
	ctx, cancel := context.WithTimeout(parent, p.timeout)
	defer cancel()

	input, err := os.CreateTemp("", "paperless-gpt-tesseract-*")
	if err != nil {
		return nil, fmt.Errorf("create tesseract input: %w", err)
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if _, err := input.Write(imageContent); err != nil {
		input.Close()
		return nil, fmt.Errorf("write tesseract input: %w", err)
	}
	if err := input.Close(); err != nil {
		return nil, fmt.Errorf("close tesseract input: %w", err)
	}

	cmd := exec.CommandContext(ctx, p.binary, inputName, "stdout", "--psm", strconv.Itoa(p.psm), "-l", p.languages, "hocr")
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = p.maxOutput, 64<<10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("tesseract timed out: %w", ctx.Err())
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return nil, fmt.Errorf("tesseract failed: %w", err)
		}
		return nil, fmt.Errorf("tesseract failed: %w: %s", err, message)
	}
	page, text, err := parseTesseractHOCR(stdout.Bytes(), pageNumber)
	if err != nil {
		return nil, fmt.Errorf("parse tesseract hOCR: %w", err)
	}
	p.mu.Lock()
	p.hocrPages = append(p.hocrPages, page)
	p.mu.Unlock()
	return &OCRResult{Text: text, HOCRPage: &page, Metadata: map[string]string{
		"provider": "tesseract", "language": p.languages, "page_number": strconv.Itoa(pageNumber),
	}}, nil
}

func parseTesseractHOCR(data []byte, pageNumber int) (hocr.Page, string, error) {
	doc, err := hocr.ParseHOCR(data)
	if err != nil {
		return hocr.Page{}, "", err
	}
	if len(doc.Pages) == 0 {
		return hocr.Page{}, "", fmt.Errorf("no OCR pages in hOCR output")
	}
	page := doc.Pages[0]
	if page.BBox.X2 <= page.BBox.X1 || page.BBox.Y2 <= page.BBox.Y1 {
		return hocr.Page{}, "", fmt.Errorf("hOCR page has no valid bounding box")
	}
	page.PageNumber = pageNumber
	var textLines []string
	for _, line := range page.Lines {
		parts := make([]string, 0, len(line.Words))
		for _, word := range line.Words {
			if strings.TrimSpace(word.Text) != "" {
				parts = append(parts, strings.TrimSpace(word.Text))
			}
		}
		if len(parts) > 0 {
			textLines = append(textLines, strings.Join(parts, " "))
		}
	}
	return page, strings.Join(textLines, "\n"), nil
}

func (p *TesseractProvider) IsHOCREnabled() bool { return true }
func (p *TesseractProvider) GetHOCRPages() []hocr.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]hocr.Page, len(p.hocrPages))
	copy(out, p.hocrPages)
	return out
}
func (p *TesseractProvider) GetHOCRDocument() (*hocr.HOCR, error) {
	pages := p.GetHOCRPages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("no hOCR pages collected")
	}
	return &hocr.HOCR{Title: "paperless-gpt tesseract", Language: p.languages, Pages: pages}, nil
}
func (p *TesseractProvider) ResetHOCR() { p.mu.Lock(); defer p.mu.Unlock(); p.hocrPages = nil }

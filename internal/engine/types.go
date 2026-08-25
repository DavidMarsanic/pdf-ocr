package engine

import "errors"

// Language is one OCR language pdf-ocr can offer, identified by the same
// 3-letter code tesseract itself uses (ISO 639-2, e.g. "eng", "fra").
type Language struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// knownLanguages is a curated set of common tesseract language codes and
// their human-facing labels. It's deliberately not an exhaustive copy of
// tesseract's ~100 available packs — just enough to cover the languages
// most people will actually reach for. Availability at runtime is
// intersected against whatever's actually installed; see detectLanguages.
var knownLanguages = []Language{
	{"eng", "English"},
	{"fra", "French"},
	{"deu", "German"},
	{"spa", "Spanish"},
	{"ita", "Italian"},
	{"por", "Portuguese"},
	{"nld", "Dutch"},
	{"swe", "Swedish"},
	{"nor", "Norwegian"},
	{"dan", "Danish"},
	{"fin", "Finnish"},
	{"pol", "Polish"},
	{"ces", "Czech"},
	{"ron", "Romanian"},
	{"hun", "Hungarian"},
	{"ell", "Greek"},
	{"rus", "Russian"},
	{"ukr", "Ukrainian"},
	{"tur", "Turkish"},
	{"ara", "Arabic"},
	{"heb", "Hebrew"},
	{"hin", "Hindi"},
	{"jpn", "Japanese"},
	{"kor", "Korean"},
	{"chi_sim", "Chinese (Simplified)"},
	{"chi_tra", "Chinese (Traditional)"},
	{"vie", "Vietnamese"},
	{"tha", "Thai"},
}

// Options carries the user's OCR choices.
type Options struct {
	Language string // tesseract language code, e.g. "eng"; empty defaults to "eng"

	// OCRExistingText controls how pages that already contain a text layer
	// are handled. False (the default) passes --skip-text: those pages are
	// left untouched and only image-only pages are OCRed — this never
	// fails, even on a PDF with mixed scanned/native-text pages. True
	// passes --force-ocr instead: every page is rasterized and re-OCRed,
	// even ones that already have text.
	OCRExistingText bool

	// Cleanup enables --deskew and --rotate-pages together — straightening
	// crooked scans and correcting page orientation before OCR runs.
	Cleanup bool
}

// Progress is streamed to the caller-supplied callback during OCR.
// ocrmypdf gives no reliable machine-readable incremental progress for a
// typical document — this is a start/done tool, not a percent-streaming
// one — mirroring document-converter's pandoc-wrapping Progress type.
type Progress struct {
	Stage   string `json:"stage"` // ocr, done
	Message string `json:"message,omitempty"`
}

// Result describes the OCRed file written to disk.
type Result struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

var (
	ErrMissingDependency   = errors.New("required tool not found")
	ErrMissingLanguageData = errors.New("OCR language data not installed")
	ErrAlreadyOCRed        = errors.New("PDF already has a text layer")
	ErrEncryptedPDF        = errors.New("PDF is password-protected")
	ErrInputFile           = errors.New("invalid input PDF")
	ErrOCRFailed           = errors.New("OCR failed")
)

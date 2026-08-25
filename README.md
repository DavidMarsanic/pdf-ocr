# PDF OCR

Make a scanned PDF searchable and selectable, entirely on this machine.
Opens as its own window.

Powered by [OCRmyPDF](https://ocrmypdf.readthedocs.io) (which runs
Tesseract under the hood) for the actual OCR work. Neither is bundled;
`ocrmypdf` is expected on `PATH`.

## Requirements

- [`ocrmypdf`](https://ocrmypdf.readthedocs.io/en/latest/installation.html)
  — on macOS, `brew install ocrmypdf` also installs Tesseract, Ghostscript,
  and qpdf as its own dependencies, so that one command is all you need.
- **A Chromium-based browser already installed**: Google Chrome, Chromium,
  Brave, Microsoft Edge, or Arc — renders the app's own UI window.

If `ocrmypdf` is missing, the app still opens — it'll tell you the moment
you try to run OCR, rather than failing silently on launch.

## Use

1. Open PDF OCR — it opens its own window.
2. Drop a PDF, or click to choose one.
3. Pick the document's language (only languages Tesseract actually has
   installed are listed — English always is; more come from
   `brew install tesseract-lang`).
4. Optionally turn on:
   - **Straighten and rotate pages automatically** — deskews crooked scans
     and corrects page orientation before OCR.
   - **Also OCR pages that already have text** — by default, pages that
     already contain a text layer are left alone (safe for a PDF that
     mixes scanned and native-text pages); this re-OCRs every page
     regardless.
5. **Run OCR.**

The result is saved to your Downloads folder as `<name> (OCR).pdf` — the
original file is never modified.

A password-protected PDF can't be OCRed directly; remove the password
first (Securexe's PDF Toolkit can do this), then try again.

## License

MIT — see [LICENSE](LICENSE).

// Package uploads writes the single stored company logo file to disk. It
// has no HTTP concerns and no dependency on the rest of vane, so it is
// unit-testable without a server (design.md).
package uploads

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// logoBaseName is the fixed file name (before extension) every stored logo
// uses - a singleton resource, so there is never more than one file to
// track or clean up (SET-10).
const logoBaseName = "logo"

// Save writes r's contents to dir as the single stored logo file, named
// logo<ext> (e.g. logo.png), removing any other logo.* file in dir first
// so exactly one logo file ever exists there (SET-10). It creates dir if
// it doesn't exist yet (lazy MkdirAll - spec.md edge case: an owner may
// never upload a logo at all, so this must not run at process startup).
// The write is atomic: it writes to a temp file inside dir and os.Renames
// it over the final target, so a concurrent request for the logo file
// never observes a half-written file.
//
// Save returns the path the app serves the file back at
// ("/uploads/logo<ext>"), not a filesystem path.
func Save(dir, ext string, r io.Reader) (servedPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("uploads: failed to create uploads dir: %w", err)
	}

	if err := removeExistingLogoFiles(dir); err != nil {
		return "", err
	}

	// A per-request-unique temp name (L19) - a fixed "logo.tmp" name meant
	// two concurrent uploads' os.Create calls raced on the same path:
	// os.Create truncates on open, so the second call could truncate the
	// first's in-progress file out from under it, and both io.Copy calls
	// could interleave writes to the same underlying file. The "." prefix
	// keeps the temp name outside removeExistingLogoFiles' "logo.*" glob
	// above, so a concurrent upload's cleanup pass can never delete
	// another upload's still-open temp file either.
	tmpFile, err := os.CreateTemp(dir, "."+logoBaseName+"-*.tmp")
	if err != nil {
		return "", fmt.Errorf("uploads: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, r); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("uploads: failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("uploads: failed to close temp file: %w", err)
	}

	finalName := logoBaseName + ext
	finalPath := filepath.Join(dir, finalName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("uploads: failed to rename temp file into place: %w", err)
	}

	return "/uploads/" + finalName, nil
}

// removeExistingLogoFiles deletes every previously stored logo.* file in
// dir (any extension), so a second Save with a different extension never
// leaves the old file behind (SET-10).
func removeExistingLogoFiles(dir string) error {
	matches, err := filepath.Glob(filepath.Join(dir, logoBaseName+".*"))
	if err != nil {
		return fmt.Errorf("uploads: failed to list existing logo files: %w", err)
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			return fmt.Errorf("uploads: failed to remove existing logo file %q: %w", match, err)
		}
	}
	return nil
}

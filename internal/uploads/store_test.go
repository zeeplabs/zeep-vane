package uploads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSave_FreshDir_CreatesDirAndWritesLogoFile asserts the lazy MkdirAll
// edge case (spec.md) and the base happy path: a first Save into a
// non-existent dir creates it and writes logo<ext> with the uploaded
// bytes.
func TestSave_FreshDir_CreatesDirAndWritesLogoFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")

	servedPath, err := Save(dir, ".png", strings.NewReader("fake-png-bytes"))
	if err != nil {
		t.Fatalf("Save() returned unexpected error: %v", err)
	}
	if servedPath != "/uploads/logo.png" {
		t.Errorf("servedPath = %q, want %q", servedPath, "/uploads/logo.png")
	}

	data, err := os.ReadFile(filepath.Join(dir, "logo.png"))
	if err != nil {
		t.Fatalf("expected logo.png to exist, ReadFile() returned error: %v", err)
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("file content = %q, want %q", string(data), "fake-png-bytes")
	}
}

// TestSave_SecondSaveDifferentExtension_RemovesFirstFile asserts SET-10:
// overwriting a logo of a different extension leaves exactly one logo.*
// file in the directory - no orphaned files accumulate.
func TestSave_SecondSaveDifferentExtension_RemovesFirstFile(t *testing.T) {
	dir := t.TempDir()

	if _, err := Save(dir, ".png", strings.NewReader("first")); err != nil {
		t.Fatalf("first Save() returned unexpected error: %v", err)
	}

	servedPath, err := Save(dir, ".svg", strings.NewReader("second"))
	if err != nil {
		t.Fatalf("second Save() returned unexpected error: %v", err)
	}
	if servedPath != "/uploads/logo.svg" {
		t.Errorf("servedPath = %q, want %q", servedPath, "/uploads/logo.svg")
	}

	matches, err := filepath.Glob(filepath.Join(dir, "logo.*"))
	if err != nil {
		t.Fatalf("Glob() returned unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("logo.* files in dir = %v, want exactly 1", matches)
	}
	if filepath.Base(matches[0]) != "logo.svg" {
		t.Errorf("remaining file = %q, want %q", filepath.Base(matches[0]), "logo.svg")
	}

	if _, err := os.Stat(filepath.Join(dir, "logo.png")); !os.IsNotExist(err) {
		t.Errorf("logo.png still exists after overwrite with a different extension, want removed")
	}
}

// TestSave_SecondSaveSameExtension_OverwritesContent confirms Save handles
// the overwrite-with-the-same-extension case too: the new content replaces
// the old, still exactly one file.
func TestSave_SecondSaveSameExtension_OverwritesContent(t *testing.T) {
	dir := t.TempDir()

	if _, err := Save(dir, ".png", strings.NewReader("first")); err != nil {
		t.Fatalf("first Save() returned unexpected error: %v", err)
	}
	if _, err := Save(dir, ".png", strings.NewReader("second")); err != nil {
		t.Fatalf("second Save() returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "logo.png"))
	if err != nil {
		t.Fatalf("ReadFile() returned unexpected error: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("file content = %q, want %q", string(data), "second")
	}

	matches, err := filepath.Glob(filepath.Join(dir, "logo.*"))
	if err != nil {
		t.Fatalf("Glob() returned unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("logo.* files in dir = %v, want exactly 1", matches)
	}
}

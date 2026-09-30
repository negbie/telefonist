package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPatchFailsOnDriftAndIsIdempotent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.c")
	if err := os.WriteFile(file, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := patch{file: file, replace: "before", with: "after"}
	if err := applyPatch(p); err != nil {
		t.Fatal(err)
	}
	if err := applyPatch(p); err != nil {
		t.Fatal("second application", err)
	}
	if err := os.WriteFile(file, []byte("unrelated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyPatch(p); err == nil {
		t.Fatal("source drift silently accepted")
	}
	if err := os.WriteFile(file, []byte("before before"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyPatch(p); err == nil {
		t.Fatal("ambiguous target accepted")
	}
}

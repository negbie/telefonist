package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

type patch struct {
	file    string
	replace string
	with    string
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: patcher <patches_file>")
	}

	patches, err := parsePatches(os.Args[1])
	if err != nil {
		log.Fatalf("parse patches: %v", err)
	}

	for _, p := range patches {
		if err := applyPatch(p); err != nil {
			log.Printf("ERROR: apply patch for %s: %v", p.file, err)
		}
	}
}

func parsePatches(filename string) ([]patch, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var (
		patches []patch
		current *patch
		mode    string // "file", "replace", "with"
	)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "file:") {
			if current != nil {
				patches = append(patches, *current)
			}
			current = &patch{file: strings.TrimSpace(strings.TrimPrefix(trimmed, "file:"))}
			mode = "file"
			continue
		}

		if strings.HasPrefix(trimmed, "replace:") {
			mode = "replace"
			continue
		}

		if strings.HasPrefix(trimmed, "with:") {
			mode = "with"
			continue
		}

		if current == nil {
			continue
		}

		// Important: we keep the exact indentation for replace and with
		switch mode {
		case "replace":
			current.replace += line + "\n"
		case "with":
			current.with += line + "\n"
		}
	}

	if current != nil {
		patches = append(patches, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return patches, nil
}

func applyPatch(p patch) error {
	if p.file == "" || p.replace == "" {
		return fmt.Errorf("invalid patch: missing file or replace content")
	}

	// Trim trailing newlines for comparison
	p.replace = strings.TrimSuffix(p.replace, "\n")
	p.with = strings.TrimSuffix(p.with, "\n")

	contentBytes, err := ioutil.ReadFile(p.file)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	content := string(contentBytes)

	if strings.Contains(content, p.with) {
		log.Printf("File %s already contains the replacement content. Skipping.", p.file)
		return nil
	}

	if !strings.Contains(content, p.replace) {
		return fmt.Errorf("target content not found in %s", p.file)
	}

	log.Printf("Patching %s...", p.file)
	newContent := strings.Replace(content, p.replace, p.with, 1)

	if err := ioutil.WriteFile(p.file, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	log.Printf("Successfully patched %s", p.file)
	return nil
}

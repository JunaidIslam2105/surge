// Package lint contains project-wide static-analysis tests that are safe to
// run in CI without any external tooling.  They have no dependencies on other
// internal packages and require no TestMain setup.
package lint_test

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestNoRawUnicodeInStringLiterals walks every .go file in the repository and
// fails if any string literal contains a raw non-ASCII character (e.g. '\u2716'
// written literally as "\u2716") instead of a \uXXXX escape sequence.
//
// Why: raw glyphs are invisible in diffs, harder to grep, and can silently
// break on terminals that do not support the relevant Unicode block.  The
// project convention is to use \uXXXX escapes in all string literals.
//
// Run in CI with:
//
//	go test ./internal/lint/...
func TestNoRawUnicodeInStringLiterals(t *testing.T) {
	root := projectRoot(t)

	// Directories to skip entirely (in addition to those starting with . or _)
	skipDirs := map[string]bool{
		"vendor":   true,
		"testdata": true,
	}

	type violation struct {
		file    string
		line    int
		col     int
		raw     rune
		literal string
	}
	var violations []violation

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		// Test files intentionally use raw Unicode as test data
		// (e.g. CJK filenames, box-drawing chars in render snapshots).
		// Only production code must use \uXXXX escapes.
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		fset := token.NewFileSet()
		file := fset.AddFile(path, -1, len(src))

		var s scanner.Scanner
		// Suppress scanner errors – malformed files are reported separately by
		// the compiler; we do not want lint noise to abort the walk.
		s.Init(file, src, func(_ token.Position, _ string) {}, 0)

		for {
			pos, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING {
				continue
			}

			// lit is the *raw source text* of the token, including surrounding
			// quotes or back-ticks.  If the programmer wrote "\u2716" in source, lit
			// will contain the actual UTF-8 bytes of that glyph.  If they wrote
			// "\u2716" in source, lit only contains ASCII bytes.
			for _, r := range lit {
				if r > 0x7F && unicode.IsPrint(r) {
					position := fset.Position(pos)
					rel, _ := filepath.Rel(root, path)
					violations = append(violations, violation{
						file:    filepath.ToSlash(rel),
						line:    position.Line,
						col:     position.Column,
						raw:     r,
						literal: lit,
					})
					// One report per literal keeps output manageable.
					break
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("walk error: %v", err)
	}

	for _, v := range violations {
		t.Errorf(
			"%s:%d:%d: raw Unicode glyph %q (\\u%04X) in string literal \u2014 use \\u%04X escape instead",
			v.file, v.line, v.col, string(v.raw), v.raw, v.raw,
		)
	}
	if len(violations) > 0 {
		t.Logf(
			"%d string literal(s) contain raw Unicode glyphs. Replace each glyph with its \\uXXXX escape sequence.",
			len(violations),
		)
	}
}

// projectRoot walks upward from the test binary's working directory until it
// finds the directory that contains go.mod, which is the module root.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod: reached filesystem root")
		}
		dir = parent
	}
}

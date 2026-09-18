package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCapLines(t *testing.T) {
	text, truncated, _ := capLines("a\nb\nc\n", 2, false)
	if text != "a\nb" || !truncated {
		t.Errorf("capLines = %q, %v", text, truncated)
	}
	text, truncated, _ = capLines("a\nb\n", 2, false)
	if text != "a\nb" || truncated {
		t.Errorf("a file that fits is not truncated: %q, %v", text, truncated)
	}
}

func TestPlainText(t *testing.T) {
	got := plainText("\tindented\x1b[31m\nthis line is far too long", 10)
	lines := strings.Split(got, "\n")
	if lines[0] != "    inden…" {
		t.Errorf("tabs expand and control chars drop: %q", lines[0])
	}
	if ansi.StringWidth(lines[1]) > 10 {
		t.Errorf("line not cut at width: %q", lines[1])
	}
}

func TestRenderFileGone(t *testing.T) {
	t.Setenv("HOME", testHome)
	got := ansi.Strip(renderFile(testHome+"/.claude/harness/x/brief.md", 40, "dark"))
	if got != "gone: ~/.claude/harness/x/brief.md" {
		t.Errorf("renderFile(missing) = %q", got)
	}
}

func TestRenderFileMarkdownAndPlain(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "PLAN.md")
	txt := filepath.Join(dir, "script.py")
	os.WriteFile(md, []byte("# Startup plan\n\nSome **bold** text.\n"), 0o644)
	os.WriteFile(txt, []byte("print('# not a heading')\n"), 0o644)

	got := ansi.Strip(renderFile(md, 60, "dark"))
	if !strings.Contains(got, "Startup plan") || strings.Contains(got, "**bold**") {
		t.Errorf("markdown must go through glamour: %q", got)
	}
	if got := ansi.Strip(renderFile(txt, 60, "dark")); got != "print('# not a heading')" {
		t.Errorf("non-markdown must stay plain: %q", got)
	}
}

func TestRenderFileCapsLongFiles(t *testing.T) {
	p := filepath.Join(t.TempDir(), "long.txt")
	os.WriteFile(p, []byte(strings.Repeat("line\n", previewMaxLines+50)), 0o644)
	got := ansi.Strip(renderFile(p, 60, "dark"))
	if n := strings.Count(got, "line"); n != previewMaxLines+1 { // +1: the "… first N lines" tail
		t.Errorf("got %d lines, want %d", n-1, previewMaxLines)
	}
	if !strings.HasSuffix(got, "… first 200 lines") {
		t.Errorf("missing truncation tail: %q", got[len(got)-30:])
	}
}

func TestRenderFileBinaryEmptyAndDir(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "blob.bin")
	empty := filepath.Join(dir, "empty.md")
	os.WriteFile(bin, []byte{0x89, 'P', 'N', 'G', 0, 0, 1}, 0o644)
	os.WriteFile(empty, nil, 0o644)
	if got := ansi.Strip(renderFile(bin, 40, "dark")); got != "(binary file)" {
		t.Errorf("binary: %q", got)
	}
	if got := ansi.Strip(renderFile(empty, 40, "dark")); got != "(empty file)" {
		t.Errorf("empty: %q", got)
	}
	if got := renderFile(dir, 40, "dark"); got == "" {
		t.Error("a directory must render an error, not crash or stay blank")
	}
}

func TestPreviewKeyChangesWithMtimeAndWidth(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	os.WriteFile(p, []byte("x"), 0o644)
	if previewKey(p, 40) == previewKey(p, 50) {
		t.Error("width must be part of the key")
	}
	if !strings.HasSuffix(previewKey(p+".missing", 40), "|0") {
		t.Error("a missing file keys with mtime 0")
	}
}

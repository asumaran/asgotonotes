package main

// File preview for the right-hand column: markdown goes through glamour,
// anything else is shown as plain text, and a missing file says so. Reading
// and rendering run as a tea.Cmd; results are cached per (path, width,
// mtime, style) so an edited file re-renders without explicit invalidation.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	previewMaxLines = 200
	previewMaxBytes = 512 * 1024
)

type previewMsg struct {
	key     string
	style   string // glamour style the render used; dropped if it changed since
	content string
}

// previewKey identifies a render. The mtime component makes stale renders
// unreachable after the file changes; a missing file has mtime 0.
func previewKey(path string, width int) string {
	var mtime int64
	if st, err := os.Stat(path); err == nil {
		mtime = st.ModTime().UnixNano()
	}
	return path + "|" + strconv.Itoa(width) + "|" + strconv.FormatInt(mtime, 10)
}

func renderPreviewCmd(path, key string, width int, style string) tea.Cmd {
	return func() tea.Msg {
		return previewMsg{key: key, style: style, content: renderFile(path, width, style)}
	}
}

func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// renderFile builds the preview of one file, capped at previewMaxLines.
func renderFile(path string, width int, style string) string {
	text, truncated, err := readHead(path)
	switch {
	case os.IsNotExist(err):
		return stGone.Render("gone: " + tildePath(path, homeDir()))
	case err != nil:
		return stError.Render(err.Error())
	case strings.ContainsRune(text, 0):
		return stDim.Render("(binary file)")
	case strings.TrimSpace(text) == "":
		return stDim.Render("(empty file)")
	}
	var out string
	if isMarkdown(path) {
		front, body := splitFrontmatter(text)
		rendered, err := renderMarkdown(body, width, style)
		if err != nil {
			rendered = plainText(body, width) // raw markdown beats nothing
		}
		if front != "" {
			// Styled line by line: lipgloss pads a multi-line block to its
			// widest line.
			lines := strings.Split(plainText(front, width), "\n")
			for i, l := range lines {
				lines[i] = stDim.Render(l)
			}
			rendered = strings.Join(lines, "\n") + "\n" + rendered
		}
		out = rendered
	} else {
		out = plainText(text, width)
	}
	if truncated {
		out += "\n" + stDim.Render("… first "+strconv.Itoa(previewMaxLines)+" lines")
	}
	return out
}

// readHead returns the first previewMaxLines lines of a file (reading at
// most previewMaxBytes) and whether anything was left out.
func readHead(path string) (text string, truncated bool, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if st.IsDir() {
		return "", false, &os.PathError{Op: "read", Path: path, Err: os.ErrInvalid}
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, previewMaxBytes+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > previewMaxBytes {
		data, truncated = data[:previewMaxBytes], true
	}
	return capLines(string(bytes.ToValidUTF8(data, []byte("?"))), previewMaxLines, truncated)
}

// splitFrontmatter separates a leading YAML block (--- ... ---) from the
// markdown body. Glamour would render it as a rule followed by a heading, so
// it is shown as plain dimmed text instead. Memory files and plans carry one.
func splitFrontmatter(text string) (front, body string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", text
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", text
	}
	closing := 4 + end + len("\n---")
	if closing < len(text) && text[closing] != '\n' {
		return "", text
	}
	return text[:closing], strings.TrimLeft(text[closing:], "\n")
}

// capLines keeps the first n lines of text.
func capLines(text string, n int, truncated bool) (string, bool, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines, truncated = lines[:n], true
	}
	return strings.Join(lines, "\n"), truncated, nil
}

// plainText prepares raw text for the viewport: tabs expanded, control
// characters dropped, lines cut at width.
func plainText(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		l = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, l)
		lines[i] = truncate(l, width)
	}
	return strings.Join(lines, "\n")
}

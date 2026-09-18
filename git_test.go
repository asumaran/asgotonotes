package main

import "testing"

func TestParseLsFiles(t *testing.T) {
	out := "README.md\x00src/pages/[id].tsx\x00docs/with space.md\x00"
	got := parseLsFiles("/wt/shop", out)
	for _, want := range []string{"/wt/shop/README.md", "/wt/shop/src/pages/[id].tsx", "/wt/shop/docs/with space.md"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d paths, want 3 (the trailing NUL is not a path)", len(got))
	}
	if n := len(parseLsFiles("/wt/shop", "")); n != 0 {
		t.Errorf("empty output must give no paths, got %d", n)
	}
}

func TestInsideRoot(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{"/wt/shop", "/wt/shop/a.md", true},
		{"/wt/shop/", "/wt/shop/a/b.md", true},
		{"/wt/shop", "/wt/shop", false},
		{"/wt/shop", "/wt/shop-other/a.md", false},
		{"/wt/shop", "/tmp/a.md", false},
	}
	for _, c := range cases {
		if got := insideRoot(c.root, c.path); got != c.want {
			t.Errorf("insideRoot(%q, %q) = %v, want %v", c.root, c.path, got, c.want)
		}
	}
}

// A group whose files all live outside the root needs no git call at all.
func TestTrackedFilesSkipsGitWithoutInsideFiles(t *testing.T) {
	tracked, inGit := trackedFiles("/nonexistent/root", []string{"/tmp/a.md", "/elsewhere/b.md"})
	if len(tracked) != 0 || inGit {
		t.Errorf("tracked=%v inGit=%v, want empty/false", tracked, inGit)
	}
}

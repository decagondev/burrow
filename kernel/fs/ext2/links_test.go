package ext2_test

// links_test.go - making links (W19-S05): fast and slow symbolic links and
// hard links, read back after a remount, checked by e2fsck and debugfs.

import (
	"errors"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"burrow/kernel/fsattr"
)

func TestMakeLinks(t *testing.T) {
	img := blank(t)
	e, done := mountRW(t, img)
	slow := strings.Repeat("deep/", 20) + "target" // past the 60 bytes of a fast link
	owner := fsattr.New{UID: 1000, GID: 1000, Mtime: when}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(e.WriteFile("file", []byte(hello), 0o644, when))
	must(e.Symlink("file", "fast", owner))
	must(e.Symlink(slow, "slow", owner))
	must(e.Link("file", "hard"))
	must(e.Lchown("fast", 1234, fsattr.Keep))
	if err := e.Link("indexed", "dir2"); !errors.Is(err, syscall.EPERM) {
		t.Errorf("a hard link to a directory: %v", err)
	}
	if err := e.Link("file", "hard"); !errors.Is(err, fs.ErrExist) {
		t.Errorf("a hard link over a name: %v", err)
	}
	done()
	fsck(t, img, "after making links")
	e, done = mountRW(t, img)
	defer done()
	for name, want := range map[string]string{"fast": "file", "slow": slow} {
		if got, err := e.ReadLink(name); err != nil || got != want {
			t.Errorf("ReadLink(%s) = %q, %v", name, got, err)
		}
	}
	if b, err := fs.ReadFile(e, "fast"); err != nil || string(b) != hello {
		t.Errorf("through the fast link: %q, %v", b, err)
	}
	info, _ := e.Lstat("file")
	if a, _ := fsattr.Of(info); a.Nlink != 2 {
		t.Errorf("file has %d links", a.Nlink)
	}
	if info, _ := e.Lstat("fast"); info.Mode() != fs.ModeSymlink|0o777 {
		t.Errorf("fast: mode %v", info.Mode())
	} else if a, _ := fsattr.Of(info); a.UID != 1234 || a.GID != 1000 {
		t.Errorf("fast: Lchown gave %d:%d", a.UID, a.GID)
	}
	if out := run(t, tool(t, "debugfs"), "-R", "stat /fast", img); !strings.Contains(out, `Fast link dest: "file"`) {
		t.Errorf("debugfs does not see a fast link:\n%s", out)
	}
	must(e.Remove("slow")) // its block goes back
	must(e.Remove("file"))
	if b, err := fs.ReadFile(e, "hard"); err != nil || string(b) != hello {
		t.Errorf("the other name after a remove: %q, %v", b, err)
	}
	done()
	fsck(t, img, "after removing links")
}

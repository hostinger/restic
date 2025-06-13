package hostinger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/restic/restic/internal/fs"
	"github.com/restic/restic/internal/test"
)

// TestRejectSymlinksOutsideScopeExcludesBySymlinkScope is for testing
// the instance of --scope-symlinks parameter
func TestRejectSymlinksOutsideScopeExcludesBySymlinkScope(t *testing.T) {
	tempDir := test.TempDir(t)

	scopePath := filepath.Join(tempDir, "foodir")

	var errs []error
	errs = append(errs, os.MkdirAll(filepath.Dir(scopePath), 0700))

	// Create some files in a temporary directory.
	files := []struct {
		path string
		incl bool
	}{
		{"42", true},
		{"foodir/foo", true},
		{"foodir/foosub/underfoo", true},
		{"bardir/bar", true},
		{"bardir/barsub/underbar", true},
	}
	for _, f := range files {
		// create directories first, then the file
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		errs = append(errs, os.MkdirAll(filepath.Dir(p), 0700))
		file, err := os.Create(p)
		errs = append(errs, err)
		errs = append(errs, file.Close())
	}

	joinFn := func(elem ...string) string {
		res := ""
		sep := string(filepath.Separator)
		for _, e := range elem {
			res += sep + filepath.FromSlash(e)
		}
		return strings.TrimPrefix(res, sep)
	}

	// Create the symlinks, some of them need to be relative
	// so can't use filepath.Join as it makes them absolute.
	symlinks := []struct {
		path       string
		targetPath string
		incl       bool
	}{
		{"123", joinFn(tempDir, "foodir/foo"), true},
		{"1234", "..", false},
		{"12345", "../..", false},

		{"foodir/insidefoo", joinFn(tempDir, "foodir/foosub/underfoo"), true},
		{"foodir/outsidefoo2", joinFn(tempDir, "foodir/.."), false},
		{"foodir/outsidefoo3", "..", false},
		{"foodir/outsidefoo4", tempDir, false},

		{"bardir/insidefoo", joinFn(tempDir, "foodir/foo"), true},
		{"bardir/insidebar", joinFn(tempDir, "bardir"), false},
		{"bardir/outsidebar", joinFn(tempDir, "bardir/../foodir/insidefoo"), true},
	}
	for _, s := range symlinks {
		path := filepath.Join(tempDir, filepath.FromSlash(s.path))
		errs = append(errs, os.Symlink(s.targetPath, path))
	}

	test.OKs(t, errs) // see if anything went wrong during the creation

	scopePath, err := filepath.EvalSymlinks(scopePath)
	test.OK(t, err)

	// create rejection function
	scopeExclude, _ := RejectSymlinksOutsideScope(scopePath)

	// test a case when the file itself is not a symlink
	// but it still should be excluded
	targetDir := filepath.Base(filepath.Dir(tempDir))
	symlinkedPath := filepath.Join(tempDir, "12345", targetDir)
	fi, err := os.Lstat(symlinkedPath)
	test.OK(t, err)

	excluded := scopeExclude(symlinkedPath, fs.ExtendedStat(fi), fs.Local{})
	if !excluded {
		t.Errorf("inclusion status of %s is wrong: want %v, got %v", symlinkedPath, false, true)
	}

	// To mock the archiver scanning walk, we create filepath.WalkFn
	// that tests against the rejection function and stores
	// the result in a map that we can test against later.
	m := make(map[string]bool)
	walk := func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		excluded := scopeExclude(p, fs.ExtendedStat(fi), fs.Local{})
		// the log message helps debugging in case the test fails
		t.Logf("%q: symlink:%t; excluded:%v", p, fi.Mode()&os.ModeSymlink == os.ModeSymlink, excluded)
		m[p] = !excluded
		return nil
	}
	// walk through the temporary file and check the error
	test.OK(t, filepath.Walk(tempDir, walk))

	// compare whether the walk gave the expected values for the test cases
	for _, f := range symlinks {
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		if m[p] != f.incl {
			t.Errorf("inclusion status of %s is wrong: want %v, got %v", f.path, f.incl, m[p])
		}
	}
}

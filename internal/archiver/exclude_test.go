package archiver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/restic/restic/internal/fs"
	"github.com/restic/restic/internal/test"
)

func TestIsExcludedByFile(t *testing.T) {
	const (
		tagFilename = "CACHEDIR.TAG"
		header      = "Signature: 8a477f597d28d172789f06886806bc55"
	)
	tests := []struct {
		name    string
		tagFile string
		content string
		want    bool
	}{
		{"NoTagfile", "", "", false},
		{"EmptyTagfile", tagFilename, "", true},
		{"UnnamedTagFile", "", header, false},
		{"WrongTagFile", "notatagfile", header, false},
		{"IncorrectSig", tagFilename, header[1:], false},
		{"ValidSig", tagFilename, header, true},
		{"ValidPlusStuff", tagFilename, header + "foo", true},
		{"ValidPlusNewlineAndStuff", tagFilename, header + "\nbar", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := test.TempDir(t)

			foo := filepath.Join(tempDir, "foo")
			err := os.WriteFile(foo, []byte("foo"), 0666)
			if err != nil {
				t.Fatalf("could not write file: %v", err)
			}
			if tc.tagFile != "" {
				tagFile := filepath.Join(tempDir, tc.tagFile)
				err = os.WriteFile(tagFile, []byte(tc.content), 0666)
				if err != nil {
					t.Fatalf("could not write tagfile: %v", err)
				}
			}
			h := header
			if tc.content == "" {
				h = ""
			}
			if got := isExcludedByFile(foo, tagFilename, h, newRejectionCache(), &fs.Local{}, func(msg string, args ...interface{}) { t.Logf(msg, args...) }); tc.want != got {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

// TestMultipleIsExcludedByFile is for testing that multiple instances of
// the --exclude-if-present parameter (or the shortcut --exclude-caches do not
// cancel each other out. It was initially written to demonstrate a bug in
// rejectIfPresent.
func TestMultipleIsExcludedByFile(t *testing.T) {
	tempDir := test.TempDir(t)

	// Create some files in a temporary directory.
	// Files in UPPERCASE will be used as exclusion triggers later on.
	// We will test the inclusion later, so we add the expected value as
	// a bool.
	files := []struct {
		path string
		incl bool
	}{
		{"42", true},

		// everything in foodir except the NOFOO tagfile
		// should not be included.
		{"foodir/NOFOO", true},
		{"foodir/foo", false},
		{"foodir/foosub/underfoo", false},

		// everything in bardir except the NOBAR tagfile
		// should not be included.
		{"bardir/NOBAR", true},
		{"bardir/bar", false},
		{"bardir/barsub/underbar", false},

		// everything in bazdir should be included.
		{"bazdir/baz", true},
		{"bazdir/bazsub/underbaz", true},
	}
	var errs []error
	for _, f := range files {
		// create directories first, then the file
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		errs = append(errs, os.MkdirAll(filepath.Dir(p), 0700))
		errs = append(errs, os.WriteFile(p, []byte(f.path), 0600))
	}
	test.OKs(t, errs) // see if anything went wrong during the creation

	// create two rejection functions, one that tests for the NOFOO file
	// and one for the NOBAR file
	fooExclude, _ := RejectIfPresent("NOFOO", nil)
	barExclude, _ := RejectIfPresent("NOBAR", nil)

	// To mock the archiver scanning walk, we create filepath.WalkFn
	// that tests against the two rejection functions and stores
	// the result in a map against we can test later.
	m := make(map[string]bool)
	walk := func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		excludedByFoo := fooExclude(p, nil, &fs.Local{})
		excludedByBar := barExclude(p, nil, &fs.Local{})
		excluded := excludedByFoo || excludedByBar
		// the log message helps debugging in case the test fails
		t.Logf("%q: %v || %v = %v", p, excludedByFoo, excludedByBar, excluded)
		m[p] = !excluded
		if excluded {
			return filepath.SkipDir
		}
		return nil
	}
	// walk through the temporary file and check the error
	test.OK(t, filepath.Walk(tempDir, walk))

	// compare whether the walk gave the expected values for the test cases
	for _, f := range files {
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		if m[p] != f.incl {
			t.Errorf("inclusion status of %s is wrong: want %v, got %v", f.path, f.incl, m[p])
		}
	}
}

// TestIsExcludedByFileSize is for testing the instance of
// --exclude-larger-than parameters
func TestIsExcludedByFileSize(t *testing.T) {
	tempDir := test.TempDir(t)

	// Create some files in a temporary directory.
	// Files in UPPERCASE will be used as exclusion triggers later on.
	// We will test the inclusion later, so we add the expected value as
	// a bool.
	files := []struct {
		path string
		size int64
		incl bool
	}{
		{"42", 100, true},

		// everything in foodir except the FOOLARGE tagfile
		// should not be included.
		{"foodir/FOOLARGE", 2048, false},
		{"foodir/foo", 1002, true},
		{"foodir/foosub/underfoo", 100, true},

		// everything in bardir except the BARLARGE tagfile
		// should not be included.
		{"bardir/BARLARGE", 1030, false},
		{"bardir/bar", 1000, true},
		{"bardir/barsub/underbar", 500, true},

		// everything in bazdir should be included.
		{"bazdir/baz", 100, true},
		{"bazdir/bazsub/underbaz", 200, true},
	}
	var errs []error
	for _, f := range files {
		// create directories first, then the file
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		errs = append(errs, os.MkdirAll(filepath.Dir(p), 0700))
		file, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		errs = append(errs, err)
		if err == nil {
			// create a file with given size
			errs = append(errs, file.Truncate(f.size))
		}
		errs = append(errs, file.Close())
	}
	test.OKs(t, errs) // see if anything went wrong during the creation

	// create rejection function
	sizeExclude, _ := RejectBySize(1024)

	// To mock the archiver scanning walk, we create filepath.WalkFn
	// that tests against the two rejection functions and stores
	// the result in a map against we can test later.
	m := make(map[string]bool)
	walk := func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		excluded := sizeExclude(p, fs.ExtendedStat(fi), nil)
		// the log message helps debugging in case the test fails
		t.Logf("%q: dir:%t; size:%d; excluded:%v", p, fi.IsDir(), fi.Size(), excluded)
		m[p] = !excluded
		return nil
	}
	// walk through the temporary file and check the error
	test.OK(t, filepath.Walk(tempDir, walk))

	// compare whether the walk gave the expected values for the test cases
	for _, f := range files {
		p := filepath.Join(tempDir, filepath.FromSlash(f.path))
		if m[p] != f.incl {
			t.Errorf("inclusion status of %s is wrong: want %v, got %v", f.path, f.incl, m[p])
		}
	}
}

// TestIsExcludedBySymlinkScope is for testing the instance of
// --scope-symlinks parameter
func TestIsExcludedBySymlinkScope(t *testing.T) {
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
	scopeExclude, _ := rejectSymlinksOutsideScope(scopePath)

	// test a case when the file itself is not a symlink
	// but it still should be excluded
	symlinkedPath := filepath.Join(tempDir, "12345", "T")
	fi, err := os.Lstat(symlinkedPath)
	test.OK(t, err)

	excluded := scopeExclude(symlinkedPath, fi)
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

		excluded := scopeExclude(p, fi)
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

func TestDeviceMap(t *testing.T) {
	deviceMap := deviceMap{
		filepath.FromSlash("/"):          1,
		filepath.FromSlash("/usr/local"): 5,
	}

	var tests = []struct {
		item     string
		deviceID uint64
		allowed  bool
	}{
		{"/root", 1, true},
		{"/usr", 1, true},

		{"/proc", 2, false},
		{"/proc/1234", 2, false},

		{"/usr", 3, false},
		{"/usr/share", 3, false},

		{"/usr/local", 5, true},
		{"/usr/local/foobar", 5, true},

		{"/usr/local/foobar/submount", 23, false},
		{"/usr/local/foobar/submount/file", 23, false},

		{"/usr/local/foobar/outhersubmount", 1, false},
		{"/usr/local/foobar/outhersubmount/otherfile", 1, false},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			res, err := deviceMap.IsAllowed(filepath.FromSlash(test.item), test.deviceID, &fs.Local{})
			if err != nil {
				t.Fatal(err)
			}

			if res != test.allowed {
				t.Fatalf("wrong result returned by IsAllowed(%v): want %v, got %v", test.item, test.allowed, res)
			}
		})
	}
}

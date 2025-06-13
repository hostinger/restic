package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/restic/restic/internal/restic"
	rtest "github.com/restic/restic/internal/test"
)

func TestRestoreSymlinkScope(t *testing.T) {
	testfiles := []struct {
		name string
		size uint
	}{
		{"testfile1.c", 100},
		{"testfile2.exe", 101},
		{"subdir1/subdir2/testfile3.docx", 102},
		{"subdir1/subdir2/testfile4.c", 102},
	}

	env, cleanup := withTestEnvironment(t)
	defer cleanup()

	testRunInit(t, env.gopts)

	for _, testFile := range testfiles {
		p := filepath.Join(env.testdata, testFile.name)
		rtest.OK(t, os.MkdirAll(filepath.Dir(p), 0755))
		rtest.OK(t, appendRandomData(p, testFile.size))
	}

	base, err := filepath.EvalSymlinks(env.base)
	rtest.OK(t, err)
	base = filepath.Join(base, "restore0")
	scope := filepath.Join(base, "testdata", "subdir1", "subdir2")

	symlinks := map[string]struct {
		target  string
		restore bool
	}{
		"symlink1":         {"./..", false},
		"subdir1/symlink2": {filepath.Join(base, "testdata", "subdir1") + "/../..", false},
		"subdir1/symlink3": {"/var", false},
		"symlink4":         {filepath.Join(base, "testdata", "testfile1.c"), false},
		"subdir1/symlink5": {filepath.Join(base, "testdata", "subdir1/subdir2/testfile3.docx"), true},
		"symlink6":         {filepath.Join(base, "testdata", "subdir1/subdir2"), true},
	}

	for name, symlink := range symlinks {
		p := filepath.Join(env.testdata, name)
		rtest.OK(t, os.Symlink(symlink.target, p))
	}

	opts := BackupOptions{}

	testRunBackup(t, filepath.Dir(env.testdata), []string{filepath.Base(env.testdata)}, opts, env.gopts)
	testRunCheck(t, env.gopts)

	snapshotID := testListSnapshots(t, env.gopts, 1)[0]

	testRunRestoreSymlinkScope(t, env.gopts, base, snapshotID, scope)
	for filename, ts := range symlinks {
		exists := testFileExists(filepath.Join(base, "testdata", filename))
		rtest.Assert(t, exists == ts.restore, "expected %v restoration status: %t, but got: %t", filename, ts.restore, exists)
	}
}

func testRunRestoreSymlinkScope(t testing.TB, gopts GlobalOptions, dir string, snapshotID restic.ID, scope string) {
	opts := RestoreOptions{
		Target:        dir,
		ScopeSymlinks: scope,
	}

	rtest.OK(t, testRunRestoreAssumeFailure(snapshotID.String(), opts, gopts))
}

func testFileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

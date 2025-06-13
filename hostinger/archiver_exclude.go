package hostinger

import (
	"path/filepath"
	"strings"

	"github.com/restic/restic/internal/archiver"
	"github.com/restic/restic/internal/debug"
	"github.com/restic/restic/internal/fs"
)

// rejectSymlinksOutsideScope rejects symlinks that target
// files outside of the specified path.
func RejectSymlinksOutsideScope(scopePath string) (archiver.RejectFunc, error) {
	var err error

	if !filepath.IsAbs(scopePath) {
		scopePath, err = filepath.Abs(scopePath)
		if err != nil {
			return nil, err
		}
	}

	return func(path string, fi *fs.ExtendedFileInfo, fs fs.FS) bool {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			// reject symlink if we cannot determine the target
			debug.Log("could not eval symlinks: %s", path)
			return true
		}

		if !strings.HasPrefix(target, scopePath) {
			debug.Log("eval path of %s (%s) is outside of scope: %s", path, target, scopePath)
			return true
		}

		return false
	}, nil
}

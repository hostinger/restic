package hostinger

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/restic/restic/internal/fs"
	"github.com/restic/restic/internal/restic"

	"github.com/restic/restic/internal/debug"
)

type NodeFilterFn func(item string, node *restic.Node) bool

func SymlinkScopeNodeFilter(scope string) NodeFilterFn {
	return func(item string, node *restic.Node) bool {
		dstdir := filepath.Dir(item)
		// if dstdir already exists eval any symlinks it may contain
		// and check if the path diverges from the scope
		if _, err := fs.Lstat(dstdir); err == nil || !os.IsNotExist(err) {
			evaldstdir, err := filepath.EvalSymlinks(dstdir)
			if err != nil {
				return false
			}

			if evaldstdir != dstdir {
				if !strings.HasPrefix(evaldstdir, scope) && !strings.HasPrefix(scope, evaldstdir) {
					debug.Log("destination dir %s is a outside scope %s", evaldstdir, scope)
					return false
				}
			}

			dstdir = evaldstdir
		}

		// node.LinkTarget can be absolute (e.g. /var/test/target) or:
		// 1. relative, with .. somewhere in the path (e.g. /var/test/target/next/..)
		// 2. relative, starting with . (e.g. ./test/target)
		//
		// Need to clean node.LinkTarget to remove abundant relative path links:
		//   /var/test/target/next/.. -> /var/test/target
		//   ./test/target -> test/target
		//
		// The path can still be relative after Clean (e.g. ./var/../../target -> ../target)
		// so we need to convert it to absolute with destination path in mind.
		// To do this, select the top destination path element that is not a file
		// and append the target to it:
		//   /restore/test/symlink -> /restore/test/../target
		//
		// and then run Clean again to remove remaining relative path links:
		//   /restore/test/../target -> /restore/target
		if node.Type == restic.NodeTypeSymlink {
			target := filepath.Clean(node.LinkTarget)
			if !filepath.IsAbs(target) {
				target = filepath.Join(dstdir, target)
			}

			if !strings.HasPrefix(target, scope) {
				debug.Log("item %s is a symlink to %s which is outside of scope %s", item, target, scope)
				return false
			}
		} else {
			target := filepath.Join(dstdir, filepath.Base(item))
			if !strings.HasPrefix(target, scope) && !strings.HasPrefix(scope, target) {
				debug.Log("item %s leads to %s which is outside of scope %s", item, target, scope)
				return false
			}
		}

		return true
	}
}

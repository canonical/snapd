// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Container is the interface to interact with the low-level snap files.
type Container interface {
	// Size returns the size of the snap in bytes.
	Size() (int64, error)

	// RandomAccessFile returns an implementation to read at any
	// given location for a single file inside the snap plus
	// information about the file size.
	RandomAccessFile(relative string) (interface {
		io.ReaderAt
		io.Closer
		Size() int64
	}, error)

	// ReadFile returns the content of a single file from the snap.
	ReadFile(relative string) ([]byte, error)

	// ReadLink returns the destination of the named symbolic link.
	ReadLink(relative string) (string, error)

	// Lstat is like os.Lstat.
	Lstat(relative string) (os.FileInfo, error)

	// Walk is like filepath.Walk, without the ordering guarantee.
	Walk(relative string, walkFn filepath.WalkFunc) error

	// ListDir returns the content of a single directory inside the snap.
	ListDir(path string) ([]string, error)

	// Install copies the snap file to targetPath (and possibly unpacks it to mountDir).
	// The bool return value indicates if the backend had nothing to do on install.
	Install(targetPath, mountDir string, opts *InstallOptions) (bool, error)

	// Unpack unpacks the src parts to the dst directory
	Unpack(src, dst string) error
}

// InstallOptions is for customizing the behavior of Install() from a higher
// level function, i.e. from overlord customizing how a snap file is installed
// on a system with tmpfs mounted as writable or with full disk encryption and
// graded secured on UC20.
type InstallOptions struct {
	// MustNotCrossDevices indicates that the snap file when installed to the
	// target must not cross devices. For example, installing a snap file from
	// the ubuntu-seed partition onto the ubuntu-data partition must result in
	// an installation on ubuntu-data that does not depend or reference
	// ubuntu-seed at all.
	MustNotCrossDevices bool
}

var (
	// ErrBadModes is returned by ValidateContainer when the container has files with the wrong file modes for their role
	ErrBadModes = errors.New("snap is unusable due to bad permissions")
	// ErrMissingPaths is returned by ValidateContainer when the container is missing required files or directories
	ErrMissingPaths = errors.New("snap is unusable due to missing files")
)

type symlinkInfo struct {
	// target is the furthest target we could evaluate.
	target string
	// targetMode is the mode of the final symlink target.
	targetMode os.FileMode
	// naiveTarget is the first symlink target.
	naiveTarget string
	// isExternal determines if the symlink is considered external
	// relative to its container.
	isExternal bool
}

// evalSymlink follows symlinks inside given container and returns
// information about it's target.
//
// The symlink is followed inside the container until we cannot
// continue further either due to absolute symlinks or symlinks
// that escape the container.
//
//       max depth reached?<------
//               /\               \
//            yes  no              \
//            /      \              \
//           V        V              \
//        error      path             \
//                    │                \
//                    V                 \
//                read target            \
//                    │                   \
//                    V                    \
//               is absolute?               \
//                   /\                      \
//                yes  no                     \
//                /      \                     \
//               V        V                     \
//       isExternal     eval relative target     \
//           +               \                    \
//     return target          V                    \
//                     escapes container?           \
//                           /\                      \
//                        yes  no                     \
//                       /      \                      |
//                      V        V                     |
//              isExternal      is symlink?            |
//                   +                /\               |
//             return target       yes  no             │
//                                /      \             │
//                               V        V            │
//                       !isExternal    path = target  │
//                            +             \----------│
//                      return target
//
func evalSymlink(c Container, path string) (symlinkInfo, error) {
	var naiveTarget string

	const maxDepth = 10
	currentDepth := 0
	for currentDepth < maxDepth {
		currentDepth++
		target, err := c.ReadLink(path)
		if err != nil {
			return symlinkInfo{}, err
		}
		// record first symlink target
		if currentDepth == 1 {
			naiveTarget = target
		}

		target = filepath.Clean(target)
		// don't follow absolute targets
		if filepath.IsAbs(target) {
			return symlinkInfo{target, os.FileMode(0), naiveTarget, true}, nil
		}

		// evaluate target relative to symlink directory
		target = filepath.Join(filepath.Dir(path), target)

		// target escapes container, cannot evaluate further, let's return
		if strings.Split(target, string(os.PathSeparator))[0] == ".." {
			return symlinkInfo{target, os.FileMode(0), naiveTarget, true}, nil
		}

		info, err := c.Lstat(target)
		// cannot follow bad targets
		if err != nil {
			return symlinkInfo{}, err
		}

		// non-symlink, let's return
		if info.Mode().Type() != os.ModeSymlink {
			return symlinkInfo{target, info.Mode(), naiveTarget, false}, nil
		}

		// we have another symlink
		path = target
	}

	return symlinkInfo{}, fmt.Errorf("too many levels of symbolic links")
}

func shouldValidateSymlink(path string) bool {
	// we only check meta directory for now
	pathTokens := strings.Split(path, string(os.PathSeparator))
	if pathTokens[0] == "meta" {
		return true
	}
	return false
}

func evalAndValidateSymlink(c Container, path string) (symlinkInfo, error) {
	pathTokens := strings.Split(path, string(os.PathSeparator))
	// check if meta directory is a symlink
	if len(pathTokens) == 1 && pathTokens[0] == "meta" {
		return symlinkInfo{}, fmt.Errorf("meta directory cannot be a symlink")
	}

	info, err := evalSymlink(c, path)
	if err != nil {
		return symlinkInfo{}, err
	}

	if info.isExternal {
		return symlinkInfo{}, fmt.Errorf("external symlink found: %s -> %s", path, info.naiveTarget)
	}

	// symlinks like this don't look innocent
	badTargets := []string{".", "meta"}
	for _, badTarget := range badTargets {
		if info.target == badTarget {
			return symlinkInfo{}, fmt.Errorf("bad symlink found: %s -> %s", path, info.naiveTarget)
		}
	}

	return info, nil
}

// ValidateComponentContainer does a minimal quick check on a snap component container.
func ValidateComponentContainer(c Container, contName string, logf func(format string, v ...interface{})) error {
	needsrx := map[string]bool{
		".":    true,
		"meta": true,
	}
	needsx := map[string]bool{}
	needsr := map[string]bool{"meta/component.yaml": true}
	needsf := map[string]bool{}
	noskipd := map[string]bool{}

	return validateContainer(c, needsrx, needsx, needsr, needsf, noskipd, "component", contName, logf)
}

// ValidateSnapContainer does a minimal quick check on a snap container.
func ValidateSnapContainer(c Container, s *Info, logf func(format string, v ...interface{})) error {
	needsrx := map[string]bool{
		".":    true,
		"meta": true,
	}
	needsx := map[string]bool{}
	needsr := map[string]bool{
		"meta/snap.yaml": true,
	}
	needsf := map[string]bool{}
	noskipd := map[string]bool{}

	for _, app := range s.Apps {
		// for non-services, paths go into the needsrx bag because users
		// need rx perms to execute it
		bag := needsrx
		paths := []string{app.Command}
		if app.IsService() {
			// services' paths just need to not be skipped by the validator
			bag = noskipd
			// additional paths to check for services:
			// XXX maybe have a method on app to keep this in sync
			paths = append(paths, app.StopCommand, app.ReloadCommand, app.PostStopCommand)
		}

		for _, path := range paths {
			path = normPath(path)
			if path == "" {
				continue
			}

			needsf[path] = true
			if app.IsService() {
				needsx[path] = true
			}
			for ; path != "."; path = filepath.Dir(path) {
				bag[path] = true
			}
		}

		// completer is special :-/
		if path := normPath(app.Completer); path != "" {
			needsr[path] = true
			for path = filepath.Dir(path); path != "."; path = filepath.Dir(path) {
				needsrx[path] = true
			}
		}
	}
	// note all needsr so far need to be regular files (or symlinks)
	for k := range needsr {
		needsf[k] = true
	}
	// thing can get jumbled up
	for path := range needsrx {
		delete(needsx, path)
		delete(needsr, path)
	}
	for path := range needsx {
		if needsr[path] {
			delete(needsx, path)
			delete(needsr, path)
			needsrx[path] = true
		}
	}

	return validateContainer(c, needsrx, needsx, needsr, needsf, noskipd, "snap", s.InstanceName(), logf)
}

// validateContainer validates data from a container. Arguments are the
// container and maps which keys are filesystem nodes and values are boolean,
// with meaning
// - needsrx tracks nodes that need to have at least 0555 perms
// - needsx tracks nodes that need to have at least 0111 perms
// - needsr tracks nodes that need to have at least 0444 perms
// - needsf tracks nodes that need to be regular files (or symlinks to regular files)
// - noskipd tracks directories we want to descend into despite not being in needs*
// The function also takes the type and name for the container (for logging
// purposes) and a log function.
func validateContainer(c Container, needsrx, needsx, needsr, needsf, noskipd map[string]bool, contType, name string, logf func(format string, v ...interface{})) error {
	seen := make(map[string]bool, len(needsx)+len(needsrx)+len(needsr))

	// bad modes are logged instead of being returned because the end user
	// can do nothing with the info (and the developer can read the logs)
	hasBadModes := false
	err := c.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		mode := info.Mode()
		if needsrx[path] || needsx[path] || needsr[path] {
			seen[path] = true
		}
		if !needsrx[path] && !needsx[path] && !needsr[path] && !strings.HasPrefix(path, "meta/") {
			if mode.IsDir() {
				if noskipd[path] {
					return nil
				}
				return filepath.SkipDir
			}
			return nil
		}

		if mode&os.ModeSymlink != 0 && shouldValidateSymlink(path) {
			symlinkInfo, err := evalAndValidateSymlink(c, path)
			if err != nil {
				logf("%s", err)
				hasBadModes = true
			} else {
				// use target mode for checks below
				mode = symlinkInfo.targetMode
			}
		}

		if mode.IsDir() {
			if mode.Perm()&0555 != 0555 {
				logf("in %s %q: %q should be world-readable and executable, and isn't: %s", contType, name, path, mode)
				hasBadModes = true
			}
		} else {
			if needsrx[path] {
				if mode.Perm()&0555 != 0555 {
					logf("in snap %q: %q should be world-readable and executable, and isn't: %s", name, path, mode)
					hasBadModes = true
				}
			}
			// XXX: do we need to match other directories?
			if needsf[path] || strings.HasPrefix(path, "meta/") {
				if mode&(os.ModeNamedPipe|os.ModeSocket|os.ModeDevice) != 0 {
					logf("in %s %q: %q should be a regular file (or a symlink) and isn't", contType, name, path)
					hasBadModes = true
				}
			}
			if needsx[path] || strings.HasPrefix(path, "meta/hooks/") {
				if mode.Perm()&0111 == 0 {
					logf("in %s %q: %q should be executable, and isn't: %s", contType, name, path, mode)
					hasBadModes = true
				}
			} else {
				// in needsr, or under meta but not a hook
				if mode.Perm()&0444 != 0444 {
					logf("in %s %q: %q should be world-readable, and isn't: %s", contType, name, path, mode)
					hasBadModes = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(needsx)+len(needsrx)+len(needsr) {
		for _, needs := range []map[string]bool{needsx, needsrx, needsr} {
			for path := range needs {
				if !seen[path] {
					logf("in %s %q: path %q does not exist", contType, name, path)
				}
			}
		}
		return ErrMissingPaths
	}

	if hasBadModes {
		return ErrBadModes
	}
	return nil
}

// normPath is a helper for validateContainer. It takes a relative path (e.g. an
// app's RestartCommand, which might be empty to mean there is no such thing),
// and cleans it.
//
//   - empty paths are returned as is
//   - if the path is not relative, it's initial / is dropped
//   - if the path goes "outside" (ie starts with ../), the empty string is
//     returned (i.e. "ignore")
//   - if there's a space in the command, ignore the rest of the string
//     (see also cmd/snap-exec/main.go's comment about strings.Split)
func normPath(path string) string {
	if path == "" {
		return ""
	}

	path = strings.TrimPrefix(filepath.Clean(path), "/")
	if strings.HasPrefix(path, "../") {
		// not something inside the snap
		return ""
	}
	if idx := strings.IndexByte(path, ' '); idx > -1 {
		return path[:idx]
	}

	return path
}

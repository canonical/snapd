// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

func dumbJoin(a, b string) string {
	if strings.HasSuffix(a, "/") {
		return a + b
	} else {
		return a + "/" + b
	}
}

func resolvePathInSysrootRec(sysroot, path string, errorOnEscape bool, symlinkRecursion int) (string, error) {
	if path == "" || path == "/" {
		// Relative paths are taken from sysroot
		return "/", nil
	}

	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	dir, file := filepath.Split(path)
	resolvedDir := mylog.Check2(resolvePathInSysrootRec(sysroot, dir, errorOnEscape, symlinkRecursion))

	if file == "" {
		return resolvedDir, nil
	}
	if file == "." {
		return resolvedDir, nil
	}
	if file == ".." {
		if errorOnEscape && (resolvedDir == "/") {
			return "", fmt.Errorf("invalid escaping path")
		}
		upperDir, _ := filepath.Split(resolvedDir)
		return upperDir, nil
	}

	fileInResolvedDir := dumbJoin(resolvedDir, file)

	realPath := dumbJoin(sysroot, fileInResolvedDir)
	st := mylog.Check2(os.Lstat(realPath))

	if st.Mode()&os.ModeSymlink != 0 {
		if symlinkRecursion < 0 {
			return "", fmt.Errorf("maximum recursion reached when reading symlinks")
		}
		target := mylog.Check2(os.Readlink(realPath))

		if filepath.IsAbs(target) {
			if errorOnEscape {
				return "", fmt.Errorf("invalid absolute symlink")
			}
			return resolvePathInSysrootRec(sysroot, target, errorOnEscape, symlinkRecursion-1)
		} else {
			return resolvePathInSysrootRec(sysroot, dumbJoin(resolvedDir, target), errorOnEscape, symlinkRecursion-1)
		}
	}

	return fileInResolvedDir, nil
}

// ResolvePathInSysroot resolves a path within a sysroot
//
// In a sysroot, abolute symlinks should be relative to the sysroot
// rather than `/`. Also paths with multiple `..` that would escape
// the sysroot should not do so.
//
// The path must point to a file that exists.
//
// Example 1:
//   - /sysroot/path1/a is a symlink pointing to /path2/b
//   - /sysroot/path2/b is a symlink pointing to /path3/c
//   - /sysroot/path3/c is a file
//     ResolvePathInSysroot("/sysroot", "/path1/a") will return "/path3/c"
//
// Example 2:
//   - /sysroot/path1/a  is a symlink pointing to ../../../path2/b
//   - /sysroot/path2/b  is a symlink pointing to ../../../path3/c
//   - /sysroot/path3/c  is a file
//     ResolvePathInSysroot("/sysroot", "../../../path1/a") will return "/path3/c"
//
// Example 3:
//   - /sysroot/path1/a is a symlink pointing to /path2/b
//   - /sysroot/path2/b does not exist
//     ResolvePathInSysroot("/sysroot", "/path1/a") will fail (IsNotExist)
//
// Example 4:
//   - /sysroot/foo is a file or a directory
//   - ResolvePathInSysroot("/sysroot", "/../../../../foo") will return "/foo"
//
// The return path is the path within the sysroot. filepath.Join() has
// to be used to get the path in the sysroot.
func ResolvePathInSysroot(sysroot, path string) (string, error) {
	return resolvePathInSysrootRec(sysroot, path, false, 255)
}

// ResolvePathNoEscape resolves a path within a pseudo sysroot
//
// Like ResolvePathInSysroot(), it resolves path as if it was a sysroot.
// However, any escaping relative path, or absolute symlink generates
// an error.
//
// The input path can however be absolute, and will be treated as
// relative.
//
// This is useful when a path is expected to be relative only and sees
// any "attempt" to escape the sysroot as a malformed path.
func ResolvePathNoEscape(sysroot, path string) (string, error) {
	return resolvePathInSysrootRec(sysroot, path, true, 255)
}

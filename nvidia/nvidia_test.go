/*
 * Copyright (C) 2024 Canonical Ltd
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

package nvidia_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snapcore/snapd/nvidia"
	"github.com/snapcore/snapd/strutil"
)

func TestAllLibrariesMatchSomePattern(t *testing.T) {
	paths, err := loadPathsFromAllLibFiles()
	if err != nil {
		t.Fatal(err)
	}

	// Globs do not contain the common prefix which is specific to distribution packaging structure.
	prefix := strutil.CommonPrefix(paths)
	t.Logf("common prefix: %v", prefix)

	for _, path := range paths {
		shortPath := strings.TrimPrefix(path, prefix)
		if !matchesAny(shortPath, nvidia.RegularGlobs) && !matchesAny(shortPath, nvidia.LibGlvndGlobs) && !matchesAny(shortPath, nvidia.ExceptionGlobs) {
			t.Errorf("Path is not matched by any glob: %s (short path: %s)", path, shortPath)
		}
	}
}

func matchesAny(path string, globs []string) bool {
	for _, glob := range globs {
		ok, err := filepath.Match(glob, path)
		if err != nil {
			panic(err) // Glob has wrong syntax.
		}
		if ok {
			return ok
		}
	}

	return false
}

func TestAllPatternsMatchSomeLibraries(t *testing.T) {
	paths, err := loadPathsFromAllLibFiles()
	if err != nil {
		t.Fatal(err)
	}

	// Globs do not contain the common prefix which is specific to distribution packaging structure.
	prefix := strutil.CommonPrefix(paths)

	for _, globList := range [][]string{nvidia.RegularGlobs, nvidia.ExceptionGlobs} {
		matched := false
		for _, pattern := range globList {
			for _, path := range paths {
				ok, err := filepath.Match(pattern, strings.TrimPrefix(path, prefix))
				if err != nil {
					t.Fatal(err)
				}

				if ok {
					matched = true
					break
				}
			}

			if !matched {
				// We have exhausted all files but the pattern did not match.
				t.Errorf("Pattern does not match any file: %s", pattern)
			}
		}
	}
}

func loadPathsFromAllLibFiles() ([]string, error) {
	libFiles, err := filepath.Glob("testdata/*.libs")
	if err != nil {
		return nil, err
	}

	// Get all the libraries from all the *.lib files.
	return loadPathsFromFiles(libFiles)
}

func loadPathsFromFiles(fileNames []string) (paths []string, err error) {
	for _, fileName := range fileNames {
		err := func() error {
			f, err := os.Open(fileName)
			if err != nil {
				return err
			}
			defer f.Close()

			sc := bufio.NewScanner(f)
			for sc.Scan() {
				path := string(bytes.TrimSpace(sc.Bytes()))
				// filter out a known packaging bug in 18.04 515 package
				if strings.HasPrefix(path, "/NVIDIA-Linux/") {
					continue
				}
				paths = append(paths, path)
			}

			return sc.Err()
		}()

		if err != nil {
			return nil, err
		}
	}

	return paths, nil
}

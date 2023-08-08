// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// Package wrappers is used to generate wrappers and service units and also desktop files for snap applications.
package wrappers

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type completionMode int

const (
	noCompletion completionMode = iota
	legacyCompletion
	normalCompletion
)

// detectCompletion verifies if and how completion should be installed.
// If `complete.sh` is not available then completion is disabled.
// If bash-completion less than 2.2, bash-completion does not support
// XDG_DATA_DIRS. So we select legacy path installation.
// If it fails to detect the version of bash-completion, it disables
// completion.
func detectCompletion(base string) (string, completionMode) {
	completeSh := dirs.CompleteShPath(base)

	if !osutil.FileExists(completeSh) {
		return "", noCompletion
	}

	fd, err := os.Open(dirs.BashCompletionScript)
	if err != nil {
		// Cannot read file, disable completion
		return "", noCompletion
	}
	defer fd.Close()

	// Up to 2.5
	releaseOld := regexp.MustCompile(`^# *RELEASE: ([0-9.]+)$`)
	// 2.6 and later
	releaseNew := regexp.MustCompile(`^BASH_COMPLETION_VERSINFO=`)
	s := bufio.NewScanner(fd)
	var matched []string
	for s.Scan() {
		line := s.Text()
		matched = releaseOld.FindStringSubmatch(line)
		if matched == nil {
			if releaseNew.MatchString(line) {
				// It must be 2.6 or later
				return completeSh, normalCompletion
			}
		} else {
			break
		}
	}
	if err := s.Err(); err != nil {
		// Cannot read file, disable completion
		return "", noCompletion
	}
	if matched == nil {
		// Unknown version: disable completion
		return "", noCompletion
	}

	versionComp, err := strutil.VersionCompare(matched[1], "2.2")
	if err != nil {
		// Cannot parse version, disable completion
		return "", noCompletion
	}

	if versionComp < 0 {
		if !osutil.IsWritable(dirs.LegacyCompletersDir) {
			return "", noCompletion
		} else {
			return completeSh, legacyCompletion
		}
	} else {
		return completeSh, normalCompletion
	}
}

func findSnapBinaryGlobs(s *snap.Info) (dirToGlobs map[string][]string) {
	dirToGlobs = make(map[string][]string)
	if s == nil {
		return dirToGlobs
	}
	for _, app := range s.Apps {
		binDir := filepath.Dir(app.WrapperPath())
		binBase := filepath.Base(app.WrapperPath())
		dirToGlobs[binDir] = append(dirToGlobs[binDir], binBase)
		if app.Completer == "" {
			continue
		}

		compDir := filepath.Dir(app.CompleterPath())
		compBase := filepath.Base(app.CompleterPath())
		if dirs.IsCompleteShSymlink(app.CompleterPath()) {
			dirToGlobs[compDir] = append(dirToGlobs[compDir], compBase)
		}

		legacyCompDir := filepath.Dir(app.LegacyCompleterPath())
		legacyCompBase := filepath.Base(app.LegacyCompleterPath())
		if dirs.IsCompleteShSymlink(app.LegacyCompleterPath()) {
			dirToGlobs[legacyCompDir] = append(dirToGlobs[legacyCompDir], legacyCompBase)
		}
	}
	return dirToGlobs
}

func EnsureSnapBinaries(oldInfo, newInfo *snap.Info) (err error) {
	dirToContent := make(map[string]map[string]osutil.FileState)
	// Initialize globs with old binaries to mark for removal
	// Removal => Present in glob set without a content entry
	dirToGlobs := findSnapBinaryGlobs(oldInfo)
	addEntry := func(dir, base string, state osutil.FileState) {
		if dirToContent[dir] == nil {
			dirToContent[dir] = make(map[string]osutil.FileState)
		}
		dirToGlobs[dir] = append(dirToGlobs[dir], base)
		if state != nil {
			dirToContent[dir][base] = state
		}
	}
	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}
	completeSh, completion := detectCompletion(newInfo.Base)

	for _, app := range newInfo.Apps {
		if app.IsService() {
			continue
		}

		binDir := filepath.Dir(app.WrapperPath())
		binBase := filepath.Base(app.WrapperPath())
		addEntry(binDir, binBase, &osutil.SymlinkFileState{Target: "/usr/bin/snap"})

		if completion == normalCompletion {
			legacyCompPath := app.LegacyCompleterPath()
			legacyCompDir := filepath.Dir(legacyCompPath)
			legacyCompBase := filepath.Base(legacyCompPath)
			if dirs.IsCompleteShSymlink(legacyCompPath) {
				// Mark legacy completion for removal
				addEntry(legacyCompDir, legacyCompBase, nil)
			}
		}

		if completion == noCompletion || app.Completer == "" {
			continue
		}
		// symlink the completion snippet
		compDir := dirs.CompletersDir
		if completion == legacyCompletion {
			compDir = dirs.LegacyCompletersDir
		}
		if err := os.MkdirAll(compDir, 0755); err != nil {
			return err
		}
		compPath := app.CompleterPath()
		if completion == legacyCompletion {
			compPath = app.LegacyCompleterPath()
		}
		if osutil.FileExists(compPath) && !dirs.IsCompleteShSymlink(compPath) {
			// Don't remove existing completer
			continue
		}
		compBase := filepath.Base(compPath)
		addEntry(compDir, compBase, &osutil.SymlinkFileState{Target: completeSh})
	}
	for dir, content := range dirToContent {
		globs := dirToGlobs[dir]
		_, _, err := osutil.EnsureDirStateGlobs(dir, globs, content)
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	// Initialize globs with old binaries to mark for removal
	// Removal => Present in glob set without a content entry
	dirToGlobs := findSnapBinaryGlobs(s)
	for dir, globs := range dirToGlobs {
		_, _, err := osutil.EnsureDirStateGlobs(dir, globs, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

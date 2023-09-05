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
	"fmt"
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

// findExistingCompleters returns a list of existing completers that were not created by us.
//
// Note: Only base names are returned and not the full paths of the completers.
func findExistingCompleters(s *snap.Info, dir string) (existingCompleters []string, err error) {
	for _, glob := range s.BinaryNameGlobs() {
		completers, err := filepath.Glob(filepath.Join(dir, glob))
		if err != nil {
			return nil, err
		}
		for _, completer := range completers {
			if osutil.FileExists(completer) && !dirs.IsCompleteShSymlink(completer) {
				existingCompleters = append(existingCompleters, filepath.Base(completer))
			}
		}
	}
	return existingCompleters, nil
}

// ensureDirStateGlobsWithKeep same as osutil.EnsureDirStateGlobs but also keeps passed files unchanged.
//
// It overwrites content by adding a self-reference to the file entries that need to be kept unchanged.
func ensureDirStateGlobsWithKeep(dir string, globs []string, content map[string]osutil.FileState, keep []string) (changed []string, removed []string, err error) {
	if len(keep) == 0 {
		return osutil.EnsureDirStateGlobs(dir, globs, content)
	}
	if content == nil {
		content = make(map[string]osutil.FileState, len(keep))
	}
	for _, file := range keep {
		content[file] = osutil.FileReference{Path: filepath.Join(dir, file)}
	}
	return osutil.EnsureDirStateGlobs(dir, globs, content)
}

// ensureSnapBinariesWithContent applies snap binary content but keeps existing completers unchanged.
func ensureSnapBinariesWithContent(s *snap.Info, binariesContent, completersContent map[string]osutil.FileState, completionVariant completionMode) error {
	// Create directories
	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}
	switch completionVariant {
	case normalCompletion:
		if err := os.MkdirAll(dirs.CompletersDir, 0755); err != nil {
			return err
		}
	case legacyCompletion:
		if err := os.MkdirAll(dirs.LegacyCompletersDir, 0755); err != nil {
			return err
		}
	}

	// Ensure binaries
	_, _, err := osutil.EnsureDirStateGlobs(dirs.SnapBinariesDir, s.BinaryNameGlobs(), binariesContent)
	if err != nil {
		return err
	}

	// Ensure completers
	// First find existing completers that were not created by us
	existingCompleters, err := findExistingCompleters(s, dirs.CompletersDir)
	if err != nil {
		return err
	}
	existingLegacyCompleters, err := findExistingCompleters(s, dirs.LegacyCompletersDir)
	if err != nil {
		return err
	}
	// Then determine which completers will be added/removed
	var normalCompletersContent, legacyCompletersContent map[string]osutil.FileState
	switch completionVariant {
	case normalCompletion:
		// Ensure completers and remove legacy completers but keep existing ones unchanged
		normalCompletersContent = completersContent
		legacyCompletersContent = nil
	case legacyCompletion:
		// Ensure legacy completers and remove completers but keep existing ones unchanged
		normalCompletersContent = nil
		legacyCompletersContent = completersContent
	default:
		// Remove both legacy completers and completers but keep existing ones unchanged
		normalCompletersContent = nil
		legacyCompletersContent = nil
	}
	// Finally add/remove completers
	_, _, err = ensureDirStateGlobsWithKeep(dirs.CompletersDir, s.BinaryNameGlobs(), normalCompletersContent, existingCompleters)
	if err != nil {
		return err
	}
	_, _, err = ensureDirStateGlobsWithKeep(dirs.LegacyCompletersDir, s.BinaryNameGlobs(), legacyCompletersContent, existingLegacyCompleters)
	if err != nil {
		return err
	}

	return nil
}

// EnsureSnapBinaries updates the wrapper binaries for the applications from the snap which aren't services.
//
// It also removes wrapper binaries from the applications of the old snap revision if it exists ensuring that
// only new snap binaries exist.
func EnsureSnapBinaries(s *snap.Info) (err error) {
	if s == nil {
		return fmt.Errorf("internal error: snap info cannot be nil")
	}
	binariesContent := map[string]osutil.FileState{}
	completersContent := map[string]osutil.FileState{}

	completeSh, completionVariant := detectCompletion(s.Base)

	for _, app := range s.Apps {
		if app.IsService() {
			continue
		}

		appBase := filepath.Base(app.WrapperPath())
		binariesContent[appBase] = &osutil.SymlinkFileState{Target: "/usr/bin/snap"}

		if completionVariant != noCompletion && app.Completer != "" {
			completersContent[appBase] = &osutil.SymlinkFileState{Target: completeSh}
		}
	}

	return ensureSnapBinariesWithContent(s, binariesContent, completersContent, completionVariant)
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	return ensureSnapBinariesWithContent(s, nil, nil, noCompletion)
}

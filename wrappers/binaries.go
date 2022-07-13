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

// AddSnapBinaries writes the wrapper binaries for the applications from the snap which aren't services.
func AddSnapBinaries(s *snap.Info) (err error) {
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, fn := range created {
			os.Remove(fn)
		}
	}()

	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}

	completeSh, completion := detectCompletion(s.Base)

	for _, app := range s.Apps {
		if app.IsService() {
			continue
		}

		wrapperPath := app.WrapperPath()
		if err := os.Remove(wrapperPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink("/usr/bin/snap", wrapperPath); err != nil {
			return err
		}
		created = append(created, wrapperPath)

		if completion == normalCompletion {
			legacyCompPath := app.LegacyCompleterPath()
			if dirs.IsCompleteShSymlink(legacyCompPath) {
				os.Remove(legacyCompPath)
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
		if err := os.Symlink(completeSh, compPath); err == nil {
			created = append(created, compPath)
		} else if !os.IsExist(err) {
			return err
		}
	}

	return nil
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	for _, app := range s.Apps {
		os.Remove(app.WrapperPath())
		if app.Completer == "" {
			continue
		}
		compPath := app.CompleterPath()
		if dirs.IsCompleteShSymlink(compPath) {
			os.Remove(compPath)
		}
		legacyCompPath := app.LegacyCompleterPath()
		if dirs.IsCompleteShSymlink(legacyCompPath) {
			os.Remove(legacyCompPath)
		}
	}

	return nil
}

// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces

import (
	"fmt"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// SecurityBackend is the common interface for security systems.
//
// Each backend is responsible for managing a specific directory and a set of
// files therein.  Files are organized in a namespace so that each there is a
// file specific to each application and but all of them can be described by a
// glob derived from the snap name.
//
// Each backend knows how to compute the desired content of all the files it
// manages. Those are handed off to a layer above all the backends to put on
// disk. Backend is notified of particular file changes and when all changes
// are complete. Some backends perform additional tasks in either of those
// moments.
type SecurityBackend interface {

	// SecuritySystem returns the name of the managed security system.
	SecuritySystem() SecuritySystem

	// Directory returns the name of the managed directory.
	Directory() string

	// FileName returns the name of security file associated with a given application.
	FileName(appInfo *snap.AppInfo) string

	// FileGlob returns the pattern describing all security files associated with a given snap.
	FileGlob(snapInfo *snap.Info) string

	// CombineSnippets combines security snippets collected from all the
	// interfaces affecting a given snap into a content map applicable to
	// EnsureDirState. The backend delegates writing those files to higher
	// layers.
	CombineSnippets(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error)

	// ObserveChanges informs the backend about changes made to the set of
	// managed files by a higher layer.
	//
	// The backend may choose to react to those changes immediately or to
	// buffer them until the higher layer signals that no more changes are
	// coming by calling FinishChanges.
	//
	// Buffering the changes is desirable when a constant cost can be incurred
	// regardless of the number of changes made.
	ObserveChanges(changed, removed []string) error

	// FinishChanges performs operations that may have been buffered by the
	// backend in reaction to a call to ObserveChanges.
	FinishChanges() error
}

// ConfigureSnapSecurity creates and loads security artefacts specific to a
// given snap. The snap can be in developer mode to make security violations
// non-fatal to the offending application process.
//
// This method should be called after changing plug, slots, connections between
// them or application present in the snap.
func ConfigureSnapSecurity(backend SecurityBackend, snapInfo *snap.Info, repo *Repository, developerMode bool) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, backend.SecuritySystem())
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	content, err := backend.CombineSnippets(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected security files for snap %q: %s", snapInfo.Name, err)
	}
	changed, removed, err := osutil.EnsureDirState(
		backend.Directory(), backend.FileGlob(snapInfo), content)
	// XXX: maybe this should not be allowed to fail?
	backend.ObserveChanges(changed, removed)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// DeconfigureSnapSecurity removes security artefacts of a given snap.
//
// This method should be called after removing a snap.
func DeconfigureSnapSecurity(backend SecurityBackend, snapInfo *snap.Info) error {
	changed, removed, err := osutil.EnsureDirState(
		backend.Directory(), backend.FileGlob(snapInfo), nil)
	if len(changed) > 0 {
		panic(fmt.Sprintf("removed snaps cannot have security files but we got %s", changed))
	}
	// XXX: maybe this should not be allowed to fail?
	backend.ObserveChanges(changed, removed)
	if err != nil {
		return fmt.Errorf("cannot synchronize security files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

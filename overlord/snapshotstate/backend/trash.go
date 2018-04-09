// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package backend

import (
	"encoding/json"
	"os"

	"github.com/snapcore/snapd/logger"
)

// Trash stores information that can be used to cleanly revert (or
// finish cleaning up) a snapshot Restore.
//
// This is useful when a Restore is part of a chain of operations, and
// a later one failing necessitates undoing the Restore.
type Trash struct {
	Done    bool     `json:"done,omitempty"`
	Created []string `json:"created,omitempty"`
	Moved   []string `json:"moved,omitempty"`
	// Config is here for convenience; this package doesn't touch it
	Config *json.RawMessage `json:"config,omitempty"`
}

// check that you're not trying to use trash info twice; data loss imminent.
func (b *Trash) check() {
	if b.Done {
		panic("attempting to use a snapshot.Trash twice")
	}
	b.Done = true
}

// Cleanup the backed up data from disk.
func (b *Trash) Cleanup() {
	b.check()
	for _, dir := range b.Moved {
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("cannot remove directory tree rooted at %q: %v", dir, err)
		}
	}
}

// Revert the backed up data: remove what was added, move back what was moved aside.
func (b *Trash) Revert() {
	b.check()
	for _, dir := range b.Created {
		logger.Debugf("removing %q", dir)
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("while undoing changes because of a previous error: cannot remove %q: %v", dir, err)
		}
	}
	for _, dir := range b.Moved {
		orig := trash2orig(dir)
		if orig == "" {
			// dir is not a trash?!?
			logger.Debugf("skipping restore of %q which seems to not be a trash", dir)
			continue
		}
		logger.Debugf("restoring %q to %q", dir, orig)
		if err := os.Rename(dir, orig); err != nil {
			logger.Noticef("while undoing changes because of a previous error: cannot restore %q to %q: %v", dir, orig, err)
		}
	}
}

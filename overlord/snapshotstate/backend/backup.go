package backend

import (
	"encoding/json"
	"os"

	"github.com/snapcore/snapd/logger"
)

// Backup stores information that can be used to cleanly revert (or
// finish cleaning up) a snapshot Restore.
//
// This is useful when a Restore is part of a chain of operations, and
// a later one failing necessitates undoing the Restore.
type Backup struct {
	Done    bool     `json:"done,omitempty"`
	Created []string `json:"created,omitempty"`
	Moved   []string `json:"moved,omitempty"`
	// Config is here for convenience; this package doesn't touch it
	Config *json.RawMessage `json:"cfg,omitempty"`
}

// check that you're not trying to use backup info twice; data loss imminent.
func (b *Backup) check() {
	if b.Done {
		panic("attempting to use a snapshot.Backup twice")
	}
	b.Done = true
}

// Cleanup the backed up data from disk.
func (b *Backup) Cleanup() {
	b.check()
	for _, dir := range b.Moved {
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("cannot remove directory tree rooted at %q: %v", dir, err)
		}
	}
}

// Revert the backed up data: remove what was added, move back what was moved aside.
func (b *Backup) Revert() {
	b.check()
	for _, dir := range b.Created {
		logger.Debugf("removing %q", dir)
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("while undoing changes because of a previous error: cannot remove %q: %v", dir, err)
		}
	}
	for _, dir := range b.Moved {
		orig := backup2orig(dir)
		if orig == "" {
			// dir is not a backup?!?
			logger.Debugf("skipping restore of %q which seems to not be a backup", dir)
			continue
		}
		logger.Debugf("restoring %q to %q", dir, orig)
		if err := os.Rename(dir, orig); err != nil {
			logger.Noticef("while undoing changes because of a previous error: cannot restore %q to %q: %v", dir, orig, err)
		}
	}
}

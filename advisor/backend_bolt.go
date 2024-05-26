// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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

package advisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
)

var (
	cmdBucketKey = []byte("Commands")
	pkgBucketKey = []byte("Snaps")
)

type writer struct {
	fn        string
	db        *bbolt.DB
	tx        *bbolt.Tx
	cmdBucket *bbolt.Bucket
	pkgBucket *bbolt.Bucket
}

// Create opens the commands database for writing, and starts a
// transaction that drops and recreates the buckets. You should then
// call AddSnap with each snap you wish to add, and them Commit the
// results to make the changes live, or Rollback to abort; either of
// these closes the database again.
func Create() (CommandDB, error) {
	t := &writer{
		fn: dirs.SnapCommandsDB + "." + randutil.RandomString(12) + "~",
	}

	t.db = mylog.Check2(bbolt.Open(t.fn, 0644, &bbolt.Options{Timeout: 1 * time.Second}))

	t.tx = mylog.Check2(t.db.Begin(true))
	if err == nil {
		t.cmdBucket = mylog.Check2(t.tx.CreateBucket(cmdBucketKey))
		if err == nil {
			t.pkgBucket = mylog.Check2(t.tx.CreateBucket(pkgBucketKey))
		}

	}

	return t, nil
}

func (t *writer) AddSnap(snapName, version, summary string, commands []string) error {
	for _, cmd := range commands {
		var sil []Package

		bcmd := []byte(cmd)
		row := t.cmdBucket.Get(bcmd)
		if row != nil {
			mylog.Check(json.Unmarshal(row, &sil))
		}
		// For the mapping of command->snap we do not need the summary, nothing is using that.
		sil = append(sil, Package{Snap: snapName, Version: version})
		row := mylog.Check2(json.Marshal(sil))
		mylog.Check(t.cmdBucket.Put(bcmd, row))

	}

	// TODO: use json here as well and put the version information here
	bj := mylog.Check2(json.Marshal(Package{
		Snap:    snapName,
		Version: version,
		Summary: summary,
	}))
	mylog.Check(t.pkgBucket.Put([]byte(snapName), bj))

	return nil
}

func (t *writer) Commit() error {
	// either everything worked, and therefore this will fail, or something
	// will fail, and that error is more important than this one if this one
	// then fails as well. So, ignore the error.
	defer os.Remove(t.fn)
	mylog.Check(t.done(true))

	dir := mylog.Check2(os.Open(filepath.Dir(dirs.SnapCommandsDB)))

	defer dir.Close()
	mylog.Check(os.Rename(t.fn, dirs.SnapCommandsDB))

	return dir.Sync()
}

func (t *writer) Rollback() error {
	e1 := t.done(false)
	e2 := os.Remove(t.fn)
	if e1 == nil {
		return e2
	}
	return e1
}

func (t *writer) done(commit bool) error {
	var e1, e2 error

	t.cmdBucket = nil
	t.pkgBucket = nil
	if t.tx != nil {
		if commit {
			e1 = t.tx.Commit()
		} else {
			e1 = t.tx.Rollback()
		}
		t.tx = nil
	}
	if t.db != nil {
		e2 = t.db.Close()
		t.db = nil
	}
	if e1 == nil {
		return e2
	}
	return e1
}

// DumpCommands returns the whole database as a map. For use in
// testing and debugging.
func DumpCommands() (map[string]string, error) {
	db := mylog.Check2(bbolt.Open(dirs.SnapCommandsDB, 0644, &bbolt.Options{
		ReadOnly: true,
		Timeout:  1 * time.Second,
	}))

	defer db.Close()

	tx := mylog.Check2(db.Begin(false))

	defer tx.Rollback()

	b := tx.Bucket(cmdBucketKey)
	if b == nil {
		return nil, nil
	}

	m := map[string]string{}
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		m[string(k)] = string(v)
	}

	return m, nil
}

type boltFinder struct {
	*bbolt.DB
}

// Open the database for reading.
func Open() (Finder, error) {
	// Check for missing file manually to workaround bug in bolt.
	// bolt.Open() is using os.OpenFile(.., os.O_RDONLY |
	// os.O_CREATE) even if ReadOnly mode is used. So we would get
	// a misleading "permission denied" error without this check.
	if !osutil.FileExists(dirs.SnapCommandsDB) {
		return nil, os.ErrNotExist
	}
	db := mylog.Check2(bbolt.Open(dirs.SnapCommandsDB, 0644, &bbolt.Options{
		ReadOnly: true,
		Timeout:  1 * time.Second,
	}))

	return &boltFinder{db}, nil
}

func (f *boltFinder) FindCommand(command string) ([]Command, error) {
	tx := mylog.Check2(f.Begin(false))

	defer tx.Rollback()

	b := tx.Bucket(cmdBucketKey)
	if b == nil {
		return nil, nil
	}

	buf := b.Get([]byte(command))
	if buf == nil {
		return nil, nil
	}
	var sil []Package
	mylog.Check(json.Unmarshal(buf, &sil))

	cmds := make([]Command, len(sil))
	for i, si := range sil {
		cmds[i] = Command{
			Snap:    si.Snap,
			Version: si.Version,
			Command: command,
		}
	}

	return cmds, nil
}

func (f *boltFinder) FindPackage(pkgName string) (*Package, error) {
	tx := mylog.Check2(f.Begin(false))

	defer tx.Rollback()

	b := tx.Bucket(pkgBucketKey)
	if b == nil {
		return nil, nil
	}

	bj := b.Get([]byte(pkgName))
	if bj == nil {
		return nil, nil
	}
	var si Package
	mylog.Check(json.Unmarshal(bj, &si))

	return &Package{Snap: pkgName, Version: si.Version, Summary: si.Summary}, nil
}

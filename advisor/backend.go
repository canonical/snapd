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

package advisor

import (
	"strings"
	"time"

	"github.com/boltdb/bolt"

	"github.com/snapcore/snapd/dirs"
)

var cmdBucketKey = []byte("Commands")

type writer struct {
	db     *bolt.DB
	tx     *bolt.Tx
	bucket *bolt.Bucket
}

type CommandDB interface {
	// AddSnap adds the entries for commands pointing to the given
	// snap name to the commands database.
	AddSnap(snapName string, commands []string) error
	// Commit persist the changes, and closes the database. If the
	// database has already been committed/rollbacked, does nothing.
	Commit() error
	// Rollback aborts the changes, and closes the database. If the
	// database has already been committed/rollbacked, does nothing.
	Rollback() error
}

// Create opens the commands database for writing, and starts a
// transaction that drops and recreates the buckets. You should then
// call AddSnap with each snap you wish to add, and them Commit the
// results to make the changes live, or Rollback to abort; either of
// these closes the database again.
func Create() (CommandDB, error) {
	var err error
	t := &writer{}

	t.db, err = bolt.Open(dirs.SnapCommandsDB, 0644, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	t.tx, err = t.db.Begin(true)
	if err == nil {
		err = t.tx.DeleteBucket(cmdBucketKey)
		if err == nil || err == bolt.ErrBucketNotFound {
			t.bucket, err = t.tx.CreateBucket(cmdBucketKey)
		}
		if err != nil {
			t.tx.Rollback()
		}
	}

	if err != nil {
		t.db.Close()
		return nil, err
	}

	return t, nil
}

func (t *writer) AddSnap(snapName string, commands []string) error {
	bname := []byte(snapName)

	for _, cmd := range commands {
		bcmd := []byte(cmd)
		row := t.bucket.Get(bcmd)
		if row == nil {
			row = bname
		} else {
			row = append(append(row, ','), bname...)
		}
		if err := t.bucket.Put(bcmd, row); err != nil {
			return err
		}
	}

	return nil
}

func (t *writer) Commit() error {
	return t.done(true)
}

func (t *writer) Rollback() error {
	return t.done(false)
}

func (t *writer) done(commit bool) error {
	var e1, e2 error

	t.bucket = nil
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

// Dump returns the whole database as a map. For use in testing and debugging.
func Dump() (map[string][]string, error) {
	db, err := bolt.Open(dirs.SnapCommandsDB, 0600, &bolt.Options{
		ReadOnly: true,
		Timeout:  1 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tx, err := db.Begin(false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	b := tx.Bucket(cmdBucketKey)
	if b == nil {
		return nil, nil
	}

	m := map[string][]string{}
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		m[string(k)] = strings.Split(string(v), ",")
	}

	return m, nil
}

type boltFinder struct {
	bolt.DB
}

// Open the database for reading.
func Open() (Finder, error) {
	db, err := bolt.Open(dirs.SnapCommandsDB, 0600, &bolt.Options{
		ReadOnly: true,
		Timeout:  1 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &boltFinder{*db}, nil
}

func (f *boltFinder) Find(command string) ([]Command, error) {
	tx, err := f.Begin(false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	b := tx.Bucket(cmdBucketKey)
	if b == nil {
		return nil, nil
	}

	buf := b.Get([]byte(command))
	if buf == nil {
		return nil, nil
	}

	snaps := strings.Split(string(buf), ",")
	cmds := make([]Command, len(snaps))
	for i, snap := range snaps {
		cmds[i] = Command{
			Snap:    snap,
			Command: command,
		}
	}

	return cmds, nil
}

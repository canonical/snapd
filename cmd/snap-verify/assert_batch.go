// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

// TODO: this is essentially a copy of assertstate.Batch, adjust and
// move that properly for sharing to asserts !!!!
// Try to merge Batch and accumFetcher as well.

import (
	"fmt"
	"io"

	"github.com/snapcore/snapd/asserts"
)

// Batch allows to accumulate a set of assertions possibly out of prerequisite order and then add them in one go to an assertion database.
type Batch struct {
	bs         asserts.Backstore
	refs       []*asserts.Ref
	linearized []asserts.Assertion
}

// NewBatch creates a new Batch to accumulate assertions to add in one go to an assertion database.
func NewBatch() *Batch {
	return &Batch{
		bs:         asserts.NewMemoryBackstore(),
		refs:       nil,
		linearized: nil,
	}
}

func (b *Batch) committing() error {
	if b.linearized != nil {
		return fmt.Errorf("internal error: cannot add to Batch while committing")
	}
	return nil
}

// Add one assertion to the batch.
func (b *Batch) Add(a asserts.Assertion) error {
	if err := b.committing(); err != nil {
		return err
	}

	if !a.SupportedFormat() {
		return &asserts.UnsupportedFormatError{Ref: a.Ref(), Format: a.Format()}
	}
	if err := b.bs.Put(a.Type(), a); err != nil {
		if revErr, ok := err.(*asserts.RevisionError); ok {
			if revErr.Current >= a.Revision() {
				// we already got something more recent
				return nil
			}
		}
		return err
	}
	b.refs = append(b.refs, a.Ref())
	return nil
}

// AddStream adds a stream of assertions to the batch.
// Returns references to to the assertions effectively added.
func (b *Batch) AddStream(r io.Reader) ([]*asserts.Ref, error) {
	if err := b.committing(); err != nil {
		return nil, err
	}

	start := len(b.refs)
	dec := asserts.NewDecoder(r)
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := b.Add(a); err != nil {
			return nil, err
		}
	}
	added := b.refs[start:]
	if len(added) == 0 {
		return nil, nil
	}
	refs := make([]*asserts.Ref, len(added))
	copy(refs, added)
	return refs, nil
}

func (b *Batch) commitTo(db *asserts.Database) error {
	if err := b.linearize(db); err != nil {
		return err
	}

	// TODO: trigger w. caller a global sanity check if something is revoked
	// (but try to save as much possible still),
	// or err is a check error
	errs := commitTo(db, b.linearized)
	if errs == nil {
		return nil
	}
	// XXX proper commitError struct
	return fmt.Errorf("cannot accept some assertions: %v", errs)
}

func (b *Batch) linearize(db *asserts.Database) error {
	if b.linearized != nil {
		return nil
	}

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		a, err := b.bs.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat())
		if asserts.IsNotFound(err) {
			// fallback to pre-existing assertions
			a, err = ref.Resolve(db.Find)
		}
		if err != nil {
			return nil, findError("cannot find %s", ref, err)
		}
		return a, nil
	}

	// linearize using accumFetcher
	f := newAccumFetcher(db, retrieve)
	for _, ref := range b.refs {
		if err := f.Fetch(ref); err != nil {
			return err
		}
	}

	b.linearized = f.fetched
	return nil
}

// CommitTo adds the batch of assertions to the given assertion database.
func (b *Batch) CommitTo(db *asserts.Database) error {
	return b.commitTo(db)
}

/*// Precheck pre-checks whether adding the batch of assertions to the given assertion database should fully succeed.
func (b *Batch) Precheck(db *asserts.Database) error {
	db = db.WithStackedBackstore(asserts.NewMemoryBackstore())

	return b.commitTo(db)
}*/

func findError(format string, ref *asserts.Ref, err error) error {
	if asserts.IsNotFound(err) {
		return fmt.Errorf(format, ref)
	} else {
		return fmt.Errorf(format+": %v", ref, err)
	}
}

// helpers

type accumFetcher struct {
	asserts.Fetcher
	fetched []asserts.Assertion
}

func newAccumFetcher(db *asserts.Database, retrieve func(*asserts.Ref) (asserts.Assertion, error)) *accumFetcher {
	f := &accumFetcher{}

	save := func(a asserts.Assertion) error {
		f.fetched = append(f.fetched, a)
		return nil
	}

	f.Fetcher = asserts.NewFetcher(db, retrieve, save)

	return f
}

func commitTo(db *asserts.Database, assertions []asserts.Assertion) (errs []error) {
	for _, a := range assertions {
		err := db.Add(a)
		if asserts.IsUnaccceptedUpdate(err) {
			if _, ok := err.(*asserts.UnsupportedFormatError); ok {
				// we kept the old one, but log the issue
				// logger.Noticef("Cannot update assertion: %v", err)
			}
			// be idempotent
			// system db has already the same or newer
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

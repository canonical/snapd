// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package asserts

import (
	"fmt"
	"io"
	"strings"
)

// Batch allows to accumulate a set of assertions possibly out of
// prerequisite order and then add them in one go to an assertion
// database.
// Nothing will be committed if there are missing prerequisites, for a full
// consistency check beforehand there is the Precheck option.
type Batch struct {
	bs    Backstore
	added []Assertion
	// added is in prereq order
	inPrereqOrder bool

	unsupported func(u *Ref, err error) error
}

// NewBatch creates a new Batch to accumulate assertions to add in one
// go to an assertion database.
// unsupported can be used to ignore/log assertions with unsupported formats,
// default behavior is to error on them.
func NewBatch(unsupported func(u *Ref, err error) error) *Batch {
	if unsupported == nil {
		unsupported = func(_ *Ref, err error) error {
			return err
		}
	}

	return &Batch{
		bs:            NewMemoryBackstore(),
		inPrereqOrder: true, // empty list is trivially so
		unsupported:   unsupported,
	}
}

// Add one assertion to the batch.
func (b *Batch) Add(a Assertion) error {
	b.inPrereqOrder = false

	if !a.SupportedFormat() {
		err := &UnsupportedFormatError{Ref: a.Ref(), Format: a.Format()}
		return b.unsupported(a.Ref(), err)
	}
	if err := b.bs.Put(a.Type(), a); err != nil {
		if revErr, ok := err.(*RevisionError); ok {
			if revErr.Current >= a.Revision() {
				// we already got something more recent
				return nil
			}
		}
		return err
	}
	b.added = append(b.added, a)
	return nil
}

// AddStream adds a stream of assertions to the batch.
// Returns references to the assertions effectively added.
func (b *Batch) AddStream(r io.Reader) ([]*Ref, error) {
	b.inPrereqOrder = false

	start := len(b.added)
	dec := NewDecoder(r)
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
	added := b.added[start:]
	if len(added) == 0 {
		return nil, nil
	}
	refs := make([]*Ref, len(added))
	for i, a := range added {
		refs[i] = a.Ref()
	}
	return refs, nil
}

// Fetch adds to the batch by invoking fetching to drive an internal
// Fetcher that was built with trustedDB and retrieve.
func (b *Batch) Fetch(trustedDB RODatabase, retrieve func(*Ref) (Assertion, error), fetching func(Fetcher) error) error {
	f := NewFetcher(trustedDB, retrieve, b.Add)
	return fetching(f)
}

func (b *Batch) precheck(db *Database) error {
	db = db.WithStackedBackstore(NewMemoryBackstore())
	return b.commitTo(db)
}

type CommitOptions struct {
	// Precheck indicates whether to do a full consistency check
	// before starting adding the batch.
	Precheck bool
}

// CommitTo adds the batch of assertions to the given assertion database.
// Nothing will be committed if there are missing prerequisites, for a full
// consistency check beforehand there is the Precheck option.
func (b *Batch) CommitTo(db *Database, opts *CommitOptions) error {
	if opts == nil {
		opts = &CommitOptions{}
	}
	if opts.Precheck {
		if err := b.precheck(db); err != nil {
			return err
		}
	}

	return b.commitTo(db)
}

// commitTo does a best effort of adding all the batch assertions to
// the target database.
func (b *Batch) commitTo(db *Database) error {
	if err := b.prereqSort(db); err != nil {
		return err
	}

	// TODO: trigger w. caller a global sanity check if something is revoked
	// (but try to save as much possible still),
	// or err is a check error

	var errs []error
	for _, a := range b.added {
		err := db.Add(a)
		if IsUnaccceptedUpdate(err) {
			// unsupported format case is handled before
			// be idempotent
			// system db has already the same or newer
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return &commitError{errs: errs}
	}
	return nil
}

func (b *Batch) prereqSort(db *Database) error {
	if b.inPrereqOrder {
		// nothing to do
		return nil
	}

	// put in prereq order using a fetcher
	ordered := make([]Assertion, 0, len(b.added))
	retrieve := func(ref *Ref) (Assertion, error) {
		a, err := b.bs.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat())
		if IsNotFound(err) {
			// fallback to pre-existing assertions
			a, err = ref.Resolve(db.Find)
		}
		if err != nil {
			return nil, resolveError("cannot resolve prerequisite assertion: %s", ref, err)
		}
		return a, nil
	}
	save := func(a Assertion) error {
		ordered = append(ordered, a)
		return nil
	}
	f := NewFetcher(db, retrieve, save)

	for _, a := range b.added {
		if err := f.Fetch(a.Ref()); err != nil {
			return err
		}
	}

	b.added = ordered
	b.inPrereqOrder = true
	return nil
}

func resolveError(format string, ref *Ref, err error) error {
	if IsNotFound(err) {
		return fmt.Errorf(format, ref)
	} else {
		return fmt.Errorf(format+": %v", ref, err)
	}
}

type commitError struct {
	errs []error
}

func (e *commitError) Error() string {
	l := []string{""}
	for _, e := range e.errs {
		l = append(l, e.Error())
	}
	return fmt.Sprintf("cannot accept some assertions:%s", strings.Join(l, "\n - "))
}

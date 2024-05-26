// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
)

type fetchProgress int

const (
	fetchNotSeen fetchProgress = iota
	fetchRetrieved
	fetchSaved
)

// To allow us to mock prerequisites of an assertion for testing.
var assertionPrereqs = func(a Assertion) []*Ref {
	return a.Prerequisites()
}

// A Fetcher helps fetching assertions and their prerequisites.
type Fetcher interface {
	// Fetch retrieves the assertion indicated by ref then its prerequisites
	// recursively, along the way saving prerequisites before dependent assertions.
	Fetch(*Ref) error
	// Save retrieves the prerequisites of the assertion recursively,
	// along the way saving them, and finally saves the assertion.
	Save(Assertion) error
}

type fetcher struct {
	db          RODatabase
	retrieve    func(*Ref) (Assertion, error)
	retrieveSeq func(*AtSequence) (Assertion, error)
	save        func(Assertion) error

	fetched map[string]fetchProgress
}

// NewFetcher creates a Fetcher which will use trustedDB to determine trusted assertions,
// will fetch assertions following prerequisites using retrieve, and then will pass
// them to save, saving prerequisites before dependent assertions.
func NewFetcher(trustedDB RODatabase, retrieve func(*Ref) (Assertion, error), save func(Assertion) error) Fetcher {
	return &fetcher{
		db:       trustedDB,
		retrieve: retrieve,
		save:     save,
		fetched:  make(map[string]fetchProgress),
	}
}

// SequenceFormingFetcher is a Fetcher with special support for fetching sequence-forming assertions through FetchSequence.
type SequenceFormingFetcher interface {
	// SequenceFormingFetcher must also implement the interface of the Fetcher.
	Fetcher

	// FetchSequence retrieves the assertion as indicated the given sequence reference. Retrieving multiple
	// sequence points of the same assertion is currently unsupported. The first sequence fetched through this
	// will be the one passed to the save callback. Any subsequent sequences fetched will not have any
	// effect and will be treated as if they've already been fetched.
	FetchSequence(*AtSequence) error
}

// NewSequenceFormingFetcher creates a SequenceFormingFetcher which will use trustedDB to determine trusted assertions,
// will fetch assertions following prerequisites using retrieve and sequence-forming assertions using retrieveSeq, and then will pass
// them to save, saving prerequisites before dependent assertions.
func NewSequenceFormingFetcher(trustedDB RODatabase, retrieve func(*Ref) (Assertion, error), retrieveSeq func(*AtSequence) (Assertion, error), save func(Assertion) error) SequenceFormingFetcher {
	return &fetcher{
		db:          trustedDB,
		retrieve:    retrieve,
		retrieveSeq: retrieveSeq,
		save:        save,
		fetched:     make(map[string]fetchProgress),
	}
}

func (f *fetcher) wasFetched(ref *Ref) (bool, error) {
	switch f.fetched[ref.Unique()] {
	case fetchSaved:
		return true, nil // nothing to do
	case fetchRetrieved:
		return false, fmt.Errorf("circular assertions are not expected: %s", ref)
	}
	return false, nil
}

func (f *fetcher) fetchPrerequisitesAndSave(key string, a Assertion) error {
	f.fetched[key] = fetchRetrieved
	for _, preref := range assertionPrereqs(a) {
		mylog.Check(f.Fetch(preref))
	}
	mylog.Check(f.fetchAccountKey(a.SignKeyID()))
	mylog.Check(f.save(a))

	f.fetched[key] = fetchSaved
	return nil
}

func (f *fetcher) chase(ref *Ref, a Assertion) error {
	// check if ref points to predefined assertion, in which case
	// there is nothing to do
	_ := mylog.Check2(ref.Resolve(f.db.FindPredefined))
	if err == nil {
		return nil
	}
	if !errors.Is(err, &NotFoundError{}) {
		return err
	}
	if ok := mylog.Check2(f.wasFetched(ref)); err != nil || ok {
		// if ok is true, then the assertion was fetched and err is nil
		return err
	}
	if a == nil {
		retrieved := mylog.Check2(f.retrieve(ref))

		a = retrieved
	}
	return f.fetchPrerequisitesAndSave(ref.Unique(), a)
}

// Fetch retrieves the assertion indicated by ref then its prerequisites
// recursively, along the way saving prerequisites before dependent assertions.
func (f *fetcher) Fetch(ref *Ref) error {
	return f.chase(ref, nil)
}

func (f *fetcher) wasSeqFetched(seq *AtSequence) (bool, error) {
	switch f.fetched[seq.Unique()] {
	case fetchSaved:
		return true, nil // nothing to do
	case fetchRetrieved:
		return false, fmt.Errorf("circular assertions are not expected: %s", seq)
	}
	return false, nil
}

func (f *fetcher) fetchSequence(seq *AtSequence) error {
	// sequence forming assertions are never predefined, so we don't check for it.
	if ok := mylog.Check2(f.wasSeqFetched(seq)); err != nil || ok {
		// if ok is true, then the assertion was fetched and err is nil
		return err
	}
	a := mylog.Check2(f.retrieveSeq(seq))

	return f.fetchPrerequisitesAndSave(seq.Unique(), a)
}

// FetchSequence retrieves the assertion as indicated by its sequence reference.
func (f *fetcher) FetchSequence(seq *AtSequence) error {
	if f.retrieveSeq == nil {
		return fmt.Errorf("cannot fetch assertion sequence point, fetcher must be created using NewSequenceFormingFetcher")
	}
	return f.fetchSequence(seq)
}

// fetchAccountKey behaves like Fetch for the account-key with the given key id.
func (f *fetcher) fetchAccountKey(keyID string) error {
	keyRef := &Ref{
		Type:       AccountKeyType,
		PrimaryKey: []string{keyID},
	}
	return f.Fetch(keyRef)
}

// Save retrieves the prerequisites of the assertion recursively,
// along the way saving them, and finally saves the assertion.
func (f *fetcher) Save(a Assertion) error {
	return f.chase(a.Ref(), a)
}

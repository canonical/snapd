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
)

type fetchProgress int

const (
	fetchNotSeen fetchProgress = iota
	fetchRetrieved
	fetchSaved
)

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

type SeqFetcher interface {
	// FetchSequence retrieves the assertion as indicated by its sequence reference.
	FetchSequence(*AtSequence) error
}

func NewSeqFetcher(trustedDB RODatabase, retrieve func(*Ref) (Assertion, error), retrieveSeq func(*AtSequence) (Assertion, error), save func(Assertion) error) SeqFetcher {
	return &fetcher{
		db:          trustedDB,
		retrieve:    retrieve,
		retrieveSeq: retrieveSeq,
		save:        save,
		fetched:     make(map[string]fetchProgress),
	}
}

func (f *fetcher) isFetched(ref *Ref) (bool, error) {
	switch f.fetched[ref.Unique()] {
	case fetchSaved:
		return true, nil // nothing to do
	case fetchRetrieved:
		return false, fmt.Errorf("circular assertions are not expected: %s", ref)
	}
	return false, nil
}

func (f *fetcher) chase(ref *Ref, a Assertion) error {
	// check if ref points to predefined assertion, in which case
	// there is nothing to do
	_, err := ref.Resolve(f.db.FindPredefined)
	if err == nil {
		return nil
	}
	if !errors.Is(err, &NotFoundError{}) {
		return err
	}
	if ok, err := f.isFetched(ref); err != nil || ok {
		// if ok is true, then the assertion was fetched and err is nil
		return err
	}
	if a == nil {
		retrieved, err := f.retrieve(ref)
		if err != nil {
			return err
		}
		a = retrieved
	}
	u := ref.Unique()
	f.fetched[u] = fetchRetrieved
	for _, preref := range a.Prerequisites() {
		if err := f.Fetch(preref); err != nil {
			return err
		}
	}
	if err := f.fetchAccountKey(a.SignKeyID()); err != nil {
		return err
	}
	if err := f.save(a); err != nil {
		return err
	}
	f.fetched[u] = fetchSaved
	return nil
}

// Fetch retrieves the assertion indicated by ref then its prerequisites
// recursively, along the way saving prerequisites before dependent assertions.
func (f *fetcher) Fetch(ref *Ref) error {
	return f.chase(ref, nil)
}

func (f *fetcher) isSeqFetched(seq *AtSequence) (bool, error) {
	u := seq.Unique()
	switch f.fetched[u] {
	case fetchSaved:
		return true, nil // nothing to do
	case fetchRetrieved:
		return false, fmt.Errorf("circular assertions are not expected: %s", seq)
	}
	return false, nil
}

func (f *fetcher) fetchSequence(seq *AtSequence) error {
	// sequence forming assertions are never predefined, so we don't check for it.
	if ok, err := f.isSeqFetched(seq); err != nil || ok {
		// if ok is true, then the assertion was fetched and err is nil
		return err
	}
	retrieved, err := f.retrieveSeq(seq)
	if err != nil {
		return err
	}
	u := seq.Unique()
	f.fetched[u] = fetchRetrieved
	if err := f.fetchAccountKey(retrieved.SignKeyID()); err != nil {
		return err
	}
	if err := f.save(retrieved); err != nil {
		return err
	}
	f.fetched[u] = fetchSaved
	return nil
}

// FetchSequence retrieves the assertion as indicated by its sequence reference.
func (f *fetcher) FetchSequence(seq *AtSequence) error {
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

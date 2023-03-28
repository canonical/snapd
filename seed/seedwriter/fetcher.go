// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package seedwriter

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
)

// SeedAssertionFetcher is a Fetcher which is designed to help with the fetching
// of assertions during seeding. It keeps track of assertions fetched, and allows
// for retrieving them at any point in time during seeding. It wraps around the
// asserts.{SequenceFormingFetcher,Fetcher} interfaces to allow for flexible
// retrieval of assertions.
type SeedAssertionFetcher interface {
	Fetch(ref *asserts.Ref) error
	FetchSequence(seq *asserts.AtSequence) error
	Save(asserts.Assertion) error
	Refs() []*asserts.Ref
	ResetRefs()
}

type assertionFetcher struct {
	fetcher asserts.Fetcher
	refs    []*asserts.Ref
}

func (af *assertionFetcher) Fetch(ref *asserts.Ref) error {
	return af.fetcher.Fetch(ref)
}

// FetchSequence attempts to cast the provided fetcher to a SequenceFormingFetcher
// to allow the use of FetchSequence.
func (af *assertionFetcher) FetchSequence(seq *asserts.AtSequence) error {
	sf, ok := af.fetcher.(asserts.SequenceFormingFetcher)
	if !ok {
		return fmt.Errorf("cannot fetch assertion sequence point, fetcher must be a SequenceFormingFetcher")
	}
	return sf.FetchSequence(seq)
}

func (af *assertionFetcher) Save(a asserts.Assertion) error {
	return af.fetcher.Save(a)
}

func (af *assertionFetcher) Refs() []*asserts.Ref {
	return af.refs
}

func (af *assertionFetcher) ResetRefs() {
	af.refs = nil
}

// A NewFetcherFunc can build a Fetcher saving to an (implicit)
// database and also calling the given additional save function.
type NewFetcherFunc func(save func(asserts.Assertion) error) asserts.Fetcher

// MakeSeedAssertionFetcher makes a SeedAssertionFetcher using newFetcher which can
// build a base Fetcher with an additional save function, to capture assertion
// references.
func MakeSeedAssertionFetcher(newFetcher NewFetcherFunc) SeedAssertionFetcher {
	var af assertionFetcher
	save := func(a asserts.Assertion) error {
		af.refs = append(af.refs, a.Ref())
		return nil
	}
	af.fetcher = newFetcher(save)
	return &af
}

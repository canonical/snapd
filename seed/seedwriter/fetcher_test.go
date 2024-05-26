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

package seedwriter_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/testutil"
)

type fetcherSuite struct {
	testutil.BaseTest
	storeSigning *assertstest.StoreStack
}

var _ = Suite(&fetcherSuite{})

func (s *fetcherSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
}

func (s *fetcherSuite) setupTestAssertion(c *C) asserts.Assertion {
	modelAs := mylog.Check2(s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "can0nical",
		"model":        "my-model-2",
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "kernel",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(s.storeSigning.Add(modelAs))

	return modelAs
}

func (s *fetcherSuite) TestAssertFetcher(c *C) {
	// Verify the basic use-case of the SeedAssertionFetcher. This
	// sets up a model assertion, and we then fetch this. Then we verify
	// that the as was added accordingly to the as-tracking.
	as := s.setupTestAssertion(c)

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	}))


	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	var newFetcherCalled int
	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		newFetcherCalled++
		return asserts.NewFetcher(db, retrieve, save)
	}

	af := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	c.Assert(af, NotNil)
	c.Check(newFetcherCalled, Equals, 1)
	mylog.

		// Fetch the model assertion, then let's verify the refs was added.
		// We expect the model, and it's account key to be added.
		Check(af.Fetch(as.Ref()))
	c.Check(err, IsNil)
	c.Assert(af.Refs(), HasLen, 2)
	c.Check(af.Refs()[0].Type, Equals, asserts.AccountKeyType)
	c.Check(af.Refs()[1].String(), Equals, "model (my-model-2; series:16 brand-id:can0nical)")
	mylog.

		// Using the default fetcher, which was not created using NewSequenceFormingFetcher,
		// FetchSequence must return us an error.
		Check(af.FetchSequence(nil))
	c.Check(err, ErrorMatches, `cannot fetch assertion sequence point, fetcher must be created using NewSequenceFormingFetcher`)

	// Clear the Refs using ResetRefs
	af.ResetRefs()
	c.Check(af.Refs(), HasLen, 0)
}

func (s *fetcherSuite) TestAssertFetcherSave(c *C) {
	// Verify that we also track references added directly via
	// SeedAssertionFetcher.Save.
	as := s.setupTestAssertion(c)

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	}))


	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	var newFetcherCalled int
	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		newFetcherCalled++
		return asserts.NewFetcher(db, retrieve, save)
	}

	af := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	c.Assert(af, NotNil)
	c.Check(newFetcherCalled, Equals, 1)
	mylog.

		// Fetch the model assertion, then let's verify the refs was added.
		Check(af.Save(as))
	c.Check(err, IsNil)
	c.Assert(af.Refs(), HasLen, 2)
	c.Check(af.Refs()[0].Type, Equals, asserts.AccountKeyType)
	c.Check(af.Refs()[1].String(), Equals, "model (my-model-2; series:16 brand-id:can0nical)")
}

type testFetcher struct{}

func (t *testFetcher) Fetch(ref *asserts.Ref) error {
	return nil
}

func (t *testFetcher) Save(a asserts.Assertion) error {
	return nil
}

func (s *fetcherSuite) TestAssertFetcherInvalidSequenceFormingFetcher(c *C) {
	// Verify that trying to use FetchSequence will produce an error if
	// a non-sequence forming assertion fetcher was created using the newFetcherFunc.
	var newFetcherCalled int
	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		newFetcherCalled++
		return &testFetcher{}
	}

	af := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	c.Assert(af, NotNil)
	c.Check(newFetcherCalled, Equals, 1)
	mylog.Check(af.FetchSequence(nil))
	c.Check(err, ErrorMatches, `cannot fetch assertion sequence point, fetcher must be a SequenceFormingFetcher`)
}

// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package assets_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/testutil"
)

type assetsTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&assetsTestSuite{})

func (s *assetsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(assets.MockCleanState())
}

func (s *assetsTestSuite) TestRegisterInternalSimple(c *C) {
	assets.RegisterInternal("foo", []byte("bar"))
	data := assets.Internal("foo")
	c.Check(data, DeepEquals, []byte("bar"))

	complexData := `this is "some
complex binary " data
`
	assets.RegisterInternal("complex-data", []byte(complexData))
	complex := assets.Internal("complex-data")
	c.Check(complex, DeepEquals, []byte(complexData))

	nodata := assets.Internal("no data")
	c.Check(nodata, IsNil)
}

func (s *assetsTestSuite) TestRegisterDoublePanics(c *C) {
	assets.RegisterInternal("foo", []byte("foo"))
	// panics with the same key, no matter the data used
	c.Assert(func() { assets.RegisterInternal("foo", []byte("bar")) },
		PanicMatches, `asset "foo" is already registered`)
	c.Assert(func() { assets.RegisterInternal("foo", []byte("foo")) },
		PanicMatches, `asset "foo" is already registered`)
}

func (s *assetsTestSuite) TestRegisterSnippetPanics(c *C) {
	assets.RegisterSnippetForEditions("foo", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("foo")},
	})
	// panics with the same key
	c.Assert(func() {
		assets.RegisterSnippetForEditions("foo", []assets.ForEditions{
			{FirstEdition: 2, Snippet: []byte("bar")},
		})
	}, PanicMatches, `edition snippets "foo" are already registered`)
	// panics when snippets aren't sorted
	c.Assert(func() {
		assets.RegisterSnippetForEditions("unsorted", []assets.ForEditions{
			{FirstEdition: 2, Snippet: []byte("two")},
			{FirstEdition: 1, Snippet: []byte("one")},
		})
	}, PanicMatches, `cannot validate snippets "unsorted": snippets must be sorted in ascending edition number order`)
	// panics when edition is repeated
	c.Assert(func() {
		assets.RegisterSnippetForEditions("doubled edition", []assets.ForEditions{
			{FirstEdition: 1, Snippet: []byte("one")},
			{FirstEdition: 2, Snippet: []byte("two")},
			{FirstEdition: 3, Snippet: []byte("three")},
			{FirstEdition: 3, Snippet: []byte("more tree")},
			{FirstEdition: 4, Snippet: []byte("four")},
		})
	}, PanicMatches, `cannot validate snippets "doubled edition": first edition 3 repeated`)
	// mix unsorted with duplicate edition
	c.Assert(func() {
		assets.RegisterSnippetForEditions("unsorted and doubled edition", []assets.ForEditions{
			{FirstEdition: 1, Snippet: []byte("one")},
			{FirstEdition: 2, Snippet: []byte("two")},
			{FirstEdition: 1, Snippet: []byte("one again")},
			{FirstEdition: 3, Snippet: []byte("more tree")},
			{FirstEdition: 4, Snippet: []byte("four")},
		})
	}, PanicMatches, `cannot validate snippets "unsorted and doubled edition": snippets must be sorted in ascending edition number order`)
}

func (s *assetsTestSuite) TestEditionSnippets(c *C) {
	assets.RegisterSnippetForEditions("foo", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("one")},
		{FirstEdition: 2, Snippet: []byte("two")},
		{FirstEdition: 3, Snippet: []byte("three")},
		{FirstEdition: 4, Snippet: []byte("four")},
		{FirstEdition: 10, Snippet: []byte("ten")},
		{FirstEdition: 20, Snippet: []byte("twenty")},
	})
	assets.RegisterSnippetForEditions("bar", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("bar one")},
		{FirstEdition: 3, Snippet: []byte("bar three")},
		// same as 3
		{FirstEdition: 5, Snippet: []byte("bar three")},
	})
	assets.RegisterSnippetForEditions("just-one", []assets.ForEditions{
		{FirstEdition: 2, Snippet: []byte("just one")},
	})

	for _, tc := range []struct {
		asset   string
		edition uint
		exp     []byte
	}{
		{"foo", 1, []byte("one")},
		{"foo", 4, []byte("four")},
		{"foo", 10, []byte("ten")},
		// still using snipped from edition 4
		{"foo", 9, []byte("four")},
		// still using snipped from edition 10
		{"foo", 11, []byte("ten")},
		{"foo", 30, []byte("twenty")},
		// different asset
		{"bar", 1, []byte("bar one")},
		{"bar", 2, []byte("bar one")},
		{"bar", 3, []byte("bar three")},
		{"bar", 4, []byte("bar three")},
		{"bar", 5, []byte("bar three")},
		{"bar", 6, []byte("bar three")},
		// nothing registered for edition 0
		{"bar", 0, nil},
		// a single snippet under this key
		{"just-one", 2, []byte("just one")},
		{"just-one", 1, nil},
		// asset not registered
		{"no asset", 1, nil},
		{"no asset", 100, nil},
	} {
		c.Logf("%q edition %v", tc.asset, tc.edition)
		snippet := assets.SnippetForEdition(tc.asset, tc.edition)
		c.Check(snippet, DeepEquals, tc.exp)
	}
}

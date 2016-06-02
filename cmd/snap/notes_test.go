// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

/*
 * Copyright (C) 2016 Canonical Ltd
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

package main_test

import (
	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

type notesSuite struct{}

var _ = check.Suite(&notesSuite{})

func (notesSuite) TestNoNotes(c *check.C) {
	c.Check((&snap.Notes{}).String(), check.Equals, "-")
}

func (notesSuite) TestNotesPrice(c *check.C) {
	c.Check((&snap.Notes{
		Price: "3.50GBP",
	}).String(), check.Equals, "3.50GBP")
}

func (notesSuite) TestNotesPrivate(c *check.C) {
	c.Check((&snap.Notes{
		Private: true,
	}).String(), check.Equals, "private")
}

func (notesSuite) TestNotesPrivateDevmode(c *check.C) {
	c.Check((&snap.Notes{
		Private:     true,
		Confinement: "devmode",
	}).String(), check.Equals, "devmode,private")
}

func (notesSuite) TestNotesOtherDevmode(c *check.C) {
	c.Check((&snap.Notes{
		DevMode: true,
		TryMode: true,
	}).String(), check.Equals, "devmode,try")
}

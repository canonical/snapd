// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"github.com/snapcore/snapd/client"
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

func (notesSuite) TestNotesDevMode(c *check.C) {
	c.Check((&snap.Notes{
		DevMode: true,
	}).String(), check.Equals, "devmode")
}

func (notesSuite) TestNotesJailMode(c *check.C) {
	c.Check((&snap.Notes{
		JailMode: true,
	}).String(), check.Equals, "jailmode")
}

func (notesSuite) TestNotesClassic(c *check.C) {
	c.Check((&snap.Notes{
		Classic: true,
	}).String(), check.Equals, "classic")
}

func (notesSuite) TestNotesTryMode(c *check.C) {
	c.Check((&snap.Notes{
		TryMode: true,
	}).String(), check.Equals, "try")
}

func (notesSuite) TestNotesDisabled(c *check.C) {
	c.Check((&snap.Notes{
		Disabled: true,
	}).String(), check.Equals, "disabled")
}

func (notesSuite) TestNotesBroken(c *check.C) {
	c.Check((&snap.Notes{
		Broken: true,
	}).String(), check.Equals, "broken")
}

func (notesSuite) TestNotesIgnoreValidation(c *check.C) {
	c.Check((&snap.Notes{
		IgnoreValidation: true,
	}).String(), check.Equals, "ignore-validation")
}

func (notesSuite) TestNotesNothing(c *check.C) {
	c.Check((&snap.Notes{}).String(), check.Equals, "-")
}

func (notesSuite) TestNotesTwo(c *check.C) {
	c.Check((&snap.Notes{
		DevMode: true,
		Broken:  true,
	}).String(), check.Matches, "(devmode,broken|broken,devmode)")
}

func (notesSuite) TestNotesFromLocal(c *check.C) {
	// Check that DevMode note is derived from DevMode flag, not DevModeConfinement type.
	c.Check(snap.NotesFromLocal(&client.Snap{DevMode: true}).DevMode, check.Equals, true)
	c.Check(snap.NotesFromLocal(&client.Snap{Confinement: client.DevModeConfinement}).DevMode, check.Equals, false)
	c.Check(snap.NotesFromLocal(&client.Snap{IgnoreValidation: true}).IgnoreValidation, check.Equals, true)
}

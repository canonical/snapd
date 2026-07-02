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

package snap_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type dependenciesSuite struct{}

var _ = Suite(&dependenciesSuite{})

func (s *dependenciesSuite) TestBasicDependencies(c *C) {
	plugs := []string{"desktop-legacy"}
	slots := []string{}
	deps, err := snap.GetDependenciesFor(plugs, slots, "core24")
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{"accessibility-legacy"})
}

func (s *dependenciesSuite) TestForbiddenDependencies(c *C) {
	plugs := []string{"desktop-legacy", "accessibility-legacy"}
	slots := []string{}
	deps, err := snap.GetDependenciesFor(plugs, slots, "core24")
	c.Assert(err, NotNil)
	c.Assert(deps, IsNil)
}

func (s *dependenciesSuite) TestSlotDependencies(c *C) {
	plugs := []string{"desktop-legacy"}
	slots := []string{"accessibility-legacy"}
	deps, err := snap.GetDependenciesFor(plugs, slots, "core24")
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{})
}

func (s *dependenciesSuite) TestNoDependencies(c *C) {
	plugs := []string{"desktop"}
	slots := []string{}
	deps, err := snap.GetDependenciesFor(plugs, slots, "core24")
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{})
}

func (s *dependenciesSuite) TestMultiDependencies(c *C) {
	plugs := []string{"dep1", "dep2"}
	slots := []string{}

	dependencyList := map[string][]snap.DependencyElement{
		"dep1": {
			{Name: "dep3"},
			{Name: "dep4"},
		},
		"dep2": {
			{Name: "dep4"},
			{Name: "dep5"},
		},
	}

	forbiddenList := []string{}

	deps, err := snap.GetDependenciesForTest(plugs, slots, "core24", dependencyList, forbiddenList)
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{"dep3", "dep4", "dep5"})
}

func (s *dependenciesSuite) TestMultiDependenciesWithMinimumBase(c *C) {
	plugs := []string{"dep1", "dep2"}
	slots := []string{}

	dependencyList := map[string][]snap.DependencyElement{
		"dep1": {
			{Name: "dep3", MinimumBase: 26},
			{Name: "dep4"},
		},
		"dep2": {
			{Name: "dep4"},
			{Name: "dep5", MinimumBase: 22},
		},
	}

	forbiddenList := []string{}

	deps, err := snap.GetDependenciesForTest(plugs, slots, "core24", dependencyList, forbiddenList)
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{"dep4", "dep5"})
}

func (s *dependenciesSuite) TestMultiDependenciesWithMaximumBase(c *C) {
	plugs := []string{"dep1", "dep2"}
	slots := []string{}

	dependencyList := map[string][]snap.DependencyElement{
		"dep1": {
			{Name: "dep3", MaximumBase: 26},
			{Name: "dep4"},
		},
		"dep2": {
			{Name: "dep4"},
			{Name: "dep5", MaximumBase: 22},
		},
	}

	forbiddenList := []string{}

	deps, err := snap.GetDependenciesForTest(plugs, slots, "core24", dependencyList, forbiddenList)
	c.Assert(err, IsNil)
	c.Assert(deps, DeepEquals, []string{"dep3", "dep4"})
}

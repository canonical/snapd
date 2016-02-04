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
	. "gopkg.in/check.v1"
)

func (s *SnapSuite) TestGrantExplicitEverything(c *C) {
	err := s.Execute([]string{
		"snap", "grant", "producer:skill", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(s.DecodedRequestBody(c), DeepEquals, map[string]interface{}{
		"action": "grant",
		"skill": map[string]interface{}{
			"snap": "producer",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "slot",
		},
	})
}

func (s *SnapSuite) TestGrantExplicitSkillImplicitSlot(c *C) {
	err := s.Execute([]string{
		"snap", "grant", "producer:skill", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(s.DecodedRequestBody(c), DeepEquals, map[string]interface{}{
		"action": "grant",
		"skill": map[string]interface{}{
			"snap": "producer",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "",
		},
	})
}

func (s *SnapSuite) TestGrantImplicitSkillExplicitSlot(c *C) {
	err := s.Execute([]string{
		"snap", "grant", "skill", "consumer:slot"})
	c.Assert(err, IsNil)
	c.Assert(s.DecodedRequestBody(c), DeepEquals, map[string]interface{}{
		"action": "grant",
		"skill": map[string]interface{}{
			"snap": "",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "slot",
		},
	})
}

func (s *SnapSuite) TestGrantImplicitSkillImplicitSlot(c *C) {
	err := s.Execute([]string{
		"snap", "grant", "skill", "consumer"})
	c.Assert(err, IsNil)
	c.Assert(s.DecodedRequestBody(c), DeepEquals, map[string]interface{}{
		"action": "grant",
		"skill": map[string]interface{}{
			"snap": "",
			"name": "skill",
		},
		"slot": map[string]interface{}{
			"snap": "consumer",
			"name": "",
		},
	})
}

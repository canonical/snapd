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

package overlord_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord"
)

func TestOverlord(t *testing.T) { TestingT(t) }

type overlordSuite struct{}

var _ = Suite(&overlordSuite{})

func (os *overlordSuite) TestNew(c *C) {
	o, err := overlord.New()
	c.Assert(err, IsNil)
	c.Check(o, NotNil)

	c.Check(o.SnapManager(), NotNil)
	c.Check(o.AssertManager(), NotNil)
	c.Check(o.SkillManager(), NotNil)
	// TODO: c.Check(o.StateJournal(), NotNil)
}

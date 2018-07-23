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

package ifacerepo_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
)

func Test(t *testing.T) { TestingT(t) }

type ifaceRepoSuite struct {
	o    *overlord.Overlord
	repo *interfaces.Repository
}

var _ = Suite(&ifaceRepoSuite{})

func (s *ifaceRepoSuite) SetUpTest(c *C) {
	s.o = overlord.Mock()
	s.repo = &interfaces.Repository{}
}

func (s *ifaceRepoSuite) TestHappy(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ifacerepo.Replace(st, s.repo)

	repo := ifacerepo.Get(st)
	c.Check(s.repo, DeepEquals, repo)
}

func (s *ifaceRepoSuite) TestGetPanics(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	c.Check(func() { ifacerepo.Get(st) }, PanicMatches, `internal error: cannot find cached interfaces repository, interface manager not initialized\?`)
}

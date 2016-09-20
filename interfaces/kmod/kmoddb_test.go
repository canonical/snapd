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

package kmod_test

import (
	"testing"

	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) {
	TestingT(t)
}

type kmodDbSuite struct {
	testutil.BaseTest
	kmoddb *kmod.KModDb
}

var _ = Suite(&kmodDbSuite{})

func (s *kmodDbSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.kmoddb = kmod.NewKModDb()
}

func (s *kmodDbSuite) TestEmpty(c *C) {
	c.Assert(s.kmoddb, NotNil)
	mods := s.kmoddb.GetUniqueModulesList()
	c.Assert(mods, HasLen, 0)
}

func (s *kmodDbSuite) TestAddModules(c *C) {
	s.kmoddb.AddModules("foo-snap", [][]byte{
		[]byte("aaa"),
		[]byte("aaa"),
	})
}

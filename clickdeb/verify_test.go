// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package clickdeb

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	. "launchpad.net/gocheck"
)

var _ = Suite(&VerifyTestSuite{})

type VerifyTestSuite struct {
	old string
}

func (s *VerifyTestSuite) SetUpTest(c *C) {
	s.old = VerifyCmd
}

func (s *VerifyTestSuite) TearDownTest(c *C) {
	VerifyCmd = s.old
}

func (s *VerifyTestSuite) mksig(exitcode int, c *C) string {
	f := filepath.Join(c.MkDir(), "fakedebsig")
	cmd := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitcode)
	c.Assert(ioutil.WriteFile(f, []byte(cmd), 0755), IsNil)
	return f
}

func (s *VerifyTestSuite) TestLocalSnapInstallDebsigVerifyFails(c *C) {
	VerifyCmd = "false"

}

func (s *VerifyTestSuite) TestReportsSuccess(c *C) {
	VerifyCmd = "true"

	c.Check(Verify("", true), IsNil)
}

func (s *VerifyTestSuite) TestReportsFailure(c *C) {
	VerifyCmd = "false"

	err := Verify("", true)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `Signature verification failed with exit status \d+`)
}

func (s *VerifyTestSuite) TestReportsOtherFailure(c *C) {
	VerifyCmd = "/no/such/thing"

	err := Verify("", true)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `Signature verification failed: .*`)
}

func (s *VerifyTestSuite) TestReportsFailureSig(c *C) {
	VerifyCmd = s.mksig(dsFailNosigs, c)

	err := Verify("", true)
	c.Assert(err, IsNil)

	err = Verify("", false)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `Signature verification failed.*`)
}

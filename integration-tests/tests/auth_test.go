// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import (
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&authSuite{})

// regression test for auth bypass bug:
// https://bugs.launchpad.net/ubuntu/+source/snapd/+bug/1571491
type authSuite struct {
	common.SnappySuite
}

func (s *authSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	user, err := user.Current()
	c.Assert(err, check.IsNil)

	content := []byte(`{"macaroon":"yummy","discharges":["some"]}`)

	authTokenPath := filepath.Join(user.HomeDir, ".snap", "auth.json")
	err = os.MkdirAll(filepath.Dir(authTokenPath), 0700)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(authTokenPath, content, 0600)
	c.Assert(err, check.IsNil)
}

func (s *authSuite) TestRegressionAuthCrash(c *check.C) {
	cmd := []string{"snap", "install", "hello-world"}
	output, _ := cli.ExecCommandErr(cmd...)
	c.Assert(output, testutil.Contains, `error: access denied`)
}

func (s *authSuite) TestRegressionAuthBypass(c *check.C) {
	cmd := []string{"snap", "connect", "foo:bar", "baz:fromp"}
	output, _ := cli.ExecCommandErr(cmd...)
	c.Assert(output, testutil.Contains, `error: access denied`)
}

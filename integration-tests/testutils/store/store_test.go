// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package store

import (
	"io/ioutil"
	"net/http"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type storeTestSuite struct {
}

var _ = Suite(&storeTestSuite{})

func (m *storeTestSuite) TestStoreURL(c *C) {
	s := NewStore()
	c.Assert(s.URL(), Equals, "http://"+defaultAddr)
}

func (m *storeTestSuite) TestStopWorks(c *C) {
	s := NewStore()

	// start
	err := s.Start()
	c.Assert(err, IsNil)

	// check that it serves content
	transport := &http.Transport{}
	client := http.Client{
		Transport: transport,
	}
	resp, err := client.Get(s.URL())
	resp.Close = true
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode, Equals, 418)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "I'm a teapot")
	resp.Body.Close()
	transport.CloseIdleConnections()

	// stop it again
	err = s.Stop()
	c.Assert(err, IsNil)
}

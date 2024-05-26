// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package store_test

import (
	"context"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/store"
)

type clientUserAgentSuite struct{}

var _ = Suite(&clientUserAgentSuite{})

func (s *clientUserAgentSuite) TestEmptyContext(c *C) {
	cua := store.ClientUserAgent(context.TODO())
	c.Assert(cua, Equals, "")
}

func (s *clientUserAgentSuite) TestWithClientUserContext(c *C) {
	req := mylog.Check2(http.NewRequest("GET", "/", nil))

	req.Header.Add("User-Agent", "some-agent")

	cua := store.WithClientUserAgent(req.Context(), req)
	c.Assert(store.ClientUserAgent(cua), Equals, "some-agent")
}

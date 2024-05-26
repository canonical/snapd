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

package client_test

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"golang.org/x/xerrors"
	. "gopkg.in/check.v1"
)

const (
	pkgID = "chatroom.ogra"
)

func (cs *clientSuite) TestClientIconCallsEndpoint(c *C) {
	_, _ = cs.cli.Icon(pkgID)
	c.Assert(cs.req.Method, Equals, "GET")
	c.Assert(cs.req.URL.Path, Equals, fmt.Sprintf("/v2/icons/%s/icon", pkgID))
}

func (cs *clientSuite) TestClientIconHttpError(c *C) {
	cs.err = errors.New("fail")
	_ := mylog.Check2(cs.cli.Icon(pkgID))
	c.Assert(err, ErrorMatches, ".*server: fail")
}

func (cs *clientSuite) TestClientIconResponseNotFound(c *C) {
	cs.status = 404
	_ := mylog.Check2(cs.cli.Icon(pkgID))
	c.Assert(err, ErrorMatches, `.*Not Found`)
}

func (cs *clientSuite) TestClientIconInvalidContentDisposition(c *C) {
	cs.header = http.Header{"Content-Disposition": {"invalid"}}
	_ := mylog.Check2(cs.cli.Icon(pkgID))
	c.Assert(err, ErrorMatches, `.*cannot determine filename`)
}

func (cs *clientSuite) TestClientIcon(c *C) {
	cs.rsp = "pixels"
	cs.header = http.Header{"Content-Disposition": {"attachment; filename=myicon.png"}}
	icon := mylog.Check2(cs.cli.Icon(pkgID))

	c.Assert(icon.Filename, Equals, "myicon.png")
	c.Assert(icon.Content, DeepEquals, []byte("pixels"))
}

func (cs *clientSuite) TestClientIconErrIsWrapped(c *C) {
	cs.err = errors.New("boom")
	_ := mylog.Check2(cs.cli.Icon("something"))
	var e xerrors.Wrapper
	c.Assert(err, Implements, &e)
}

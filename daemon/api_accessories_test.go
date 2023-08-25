// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package daemon_test

import (
	"fmt"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
)

var _ = Suite(&accessoriesSuite{})

type accessoriesSuite struct {
	apiBaseSuite
}

func (s *accessoriesSuite) expectThemesAccess() {
	s.expectReadAccess(daemon.InterfaceOpenAccess{Interface: "snap-themes-control"})
}

func (s *accessoriesSuite) TestChangeInfo(c *C) {
	s.expectThemesAccess()

	d := s.daemon(c)
	st := d.Overlord().State()
	st.Lock()
	chg1 := st.NewChange("install-themes", "Installing a theme")
	chg2 := st.NewChange("other", "Other change")
	st.Unlock()

	// Access to install-themes changes is allowed
	req := httptest.NewRequest("GET", "/v2/accessories/changes/"+chg1.ID(), nil)
	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	info, ok := rsp.Result.(*daemon.ChangeInfo)
	c.Assert(ok, Equals, true)
	c.Check(info.ID, Equals, chg1.ID())
	c.Check(info.Kind, Equals, "install-themes")
	c.Check(info.Summary, Equals, "Installing a theme")

	// Other changes are treated as missing
	req = httptest.NewRequest("GET", "/v2/accessories/changes/"+chg2.ID(), nil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Message, Equals, fmt.Sprintf("cannot find change with id %q", chg2.ID()))
}

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
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snapdtool"
)

func (s *SnapSuite) TestVersionCommandOnClassic(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"on-classic":true,"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89","architecture":"ia64"}}`)
	})
	restore := mockArgs("snap", "version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"version"}))

	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\nubuntu  12.34\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnAllSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89","architecture":"powerpc","virtualization":"qemu"}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"version"}))

	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnClassicNoOsVersion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"on-classic": true,"os-release":{"id":"arch","version-id":""},"series":"56","version":"7.89"}}`)
	})
	restore := mockArgs("snap", "version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"version"}))

	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\narch    -\n")
	c.Assert(s.Stderr(), Equals, "")
}

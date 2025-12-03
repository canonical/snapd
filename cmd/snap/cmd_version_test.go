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
	"errors"
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

func (s *SnapSuite) TestVersionCommandOnClassic(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{`+
			`"on-classic":true,"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89",`+
			`"architecture":"ia64", "snapd-bin-from": "native-package"`+
			`}}`)
	})
	restore := mockArgs("snap", "version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap          4.56\n"+
		"snapd         7.89\n"+
		"series        56\n"+
		"ubuntu        12.34\n"+
		"architecture  ia64\n",
	)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"version", "--verbose"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap            4.56\n"+
		"snapd           7.89\n"+
		"series          56\n"+
		"ubuntu          12.34\n"+
		"architecture    ia64\n"+
		"snapd-bin-from  native-package\n"+
		"snap-bin-from   snap\n",
	)
	c.Assert(s.Stderr(), Equals, "")

	defer main.MockSnapdtoolIsReexecd(func() (bool, error) { return false, nil })()

	s.ResetStdStreams()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"version", "--verbose"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap            4.56\n"+
		"snapd           7.89\n"+
		"series          56\n"+
		"ubuntu          12.34\n"+
		"architecture    ia64\n"+
		"snapd-bin-from  native-package\n"+
		"snap-bin-from   native-package\n",
	)
	c.Assert(s.Stderr(), Equals, "")

	defer main.MockSnapdtoolIsReexecd(func() (bool, error) { return false, errors.New("mock error") })()

	s.ResetStdStreams()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"version", "--verbose"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap            4.56\n"+
		"snapd           7.89\n"+
		"series          56\n"+
		"ubuntu          12.34\n"+
		"architecture    ia64\n"+
		"snapd-bin-from  native-package\n"+
		"snap-bin-from   -\n",
	)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnAllSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{`+
			`"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89","architecture":"powerpc","virtualization":"qemu",`+
			`"snapd-bin-from": "snap"`+
			`}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap          4.56\n"+
		"snapd         7.89\n"+
		"series        56\n"+
		"architecture  powerpc\n",
	)
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"version", "--verbose"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap            4.56\n"+
		"snapd           7.89\n"+
		"series          56\n"+
		"architecture    powerpc\n"+
		"snapd-bin-from  snap\n"+
		"snap-bin-from   snap\n",
	)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnClassicNoOsVersion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{`+
			`"on-classic": true,"os-release":{"id":"arch","version-id":""},"series":"56","version":"7.89"`+
			`}}`)
	})
	restore := mockArgs("snap", "version")
	defer restore()
	restore = snapdtool.MockVersion("4.56")
	defer restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"snap    4.56\n"+
		"snapd   7.89\n"+
		"series  56\n"+
		"arch    -\n",
	)
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnWSL1(c *C) {
	defer MockWSL(1)()
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Error("This should not talk to snapd")
	})
	defer mockArgs("snap", "version")()
	defer snapdtool.MockVersion("4.56")()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), testutil.Contains, "unavailable\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionCommandOnWSL2(c *C) {
	defer MockWSL(2)()
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"on-classic":true,"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89","architecture":"ia64"}}`)
	})
	defer mockArgs("snap", "version")()
	defer snapdtool.MockVersion("4.56")()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Not(testutil.Contains), "unavailable\n")
	c.Assert(s.Stderr(), Equals, "")
}

func MockWSL(version int) (restore func()) {
	oldVersion := release.WSLVersion
	oldFlag := release.OnWSL

	release.OnWSL = true
	release.WSLVersion = version

	return func() {
		release.WSLVersion = oldVersion
		release.OnWSL = oldFlag
	}
}

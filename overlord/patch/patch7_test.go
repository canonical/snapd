// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package patch_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
)

type patch7Suite struct{}

var _ = Suite(&patch7Suite{})

var statePatch7JSON = []byte(`
{
	"data": {
		"patch-level": 6,
		"snap-cookies": {
			"02MyWT3t4StSpV7X9hT9srxj7qJm5plGN6q7HjNh6l4l": "core",
			"0PTn1YbRbxw2hC5gXJ1BbJtMwSc8fkVPtdRR3tQcq8D4": "core",
			"0bhfKPF0xX638s9pVxKPNjmrTXX09Tmrb7Ycj04t5CVN": "ubuntu-image"
		}
	}
}
`)

func (s *patch7Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch7JSON, 0644)
	c.Assert(err, IsNil)

	c.Assert(os.MkdirAll(dirs.SnapCookieDir, 0700), IsNil)
	c.Assert(ioutil.WriteFile(fmt.Sprintf("%s/snap.foo", dirs.SnapCookieDir), []byte{}, 0644), IsNil)
}

func (s *patch7Suite) TestPatch7(c *C) {
	restorer := patch.MockLevel(7)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	// go from patch level 6 -> 7
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var cookies map[string]string
	c.Assert(st.Get("snap-cookies", &cookies), IsNil)
	c.Assert(cookies, HasLen, 0)

	//TODO: files removed
}

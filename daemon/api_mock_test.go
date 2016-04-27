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

package daemon

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/snap"
)

func (s *apiSuite) mockSnap(c *C, yamlText string) *snap.Info {
	if s.d == nil {
		panic("call s.daemon(c) in your test first")
	}
	st := s.d.overlord.State()

	st.Lock()
	defer st.Unlock()

	// Parse the yaml
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	snap.AddImplicitSlots(snapInfo)

	// Create on-disk yaml file (it is read by snapstate)
	dname := filepath.Join(dirs.SnapSnapsDir, snapInfo.Name(),
		strconv.Itoa(snapInfo.Revision), "meta")
	fname := filepath.Join(dname, "snap.yaml")
	err = os.MkdirAll(dname, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fname, []byte(yamlText), 0644)
	c.Assert(err, IsNil)

	// Put a side info into the state
	snapstate.Set(st, snapInfo.Name(), &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{Revision: snapInfo.Revision}},
	})

	// Put the snap into the interface repository
	repo := s.d.overlord.InterfaceManager().Repository()
	err = repo.AddSnap(snapInfo)
	c.Assert(err, IsNil)
	return snapInfo
}

func (s *apiSuite) mockIface(c *C, iface interfaces.Interface) {
	if s.d == nil {
		panic("call s.daemon(c) in your test first")
	}
	err := s.d.overlord.InterfaceManager().Repository().AddInterface(iface)
	c.Assert(err, IsNil)
}

var consumerYaml = `
name: consumer
version: 1
apps:
 app:
plugs:
 plug:
  interface: test
  key: value
  label: label
`

var producerYaml = `
name: producer
version: 1
apps:
 app:
slots:
 slot:
  interface: test
  key: value
  label: label
`

var differentProducerYaml = `
name: producer
version: 1
apps:
 app:
slots:
 slot:
  interface: different
  key: value
  label: label
`

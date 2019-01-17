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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func (s *apiSuite) mockSnap(c *C, yamlText string) *snap.Info {
	if s.d == nil {
		panic("call s.daemon(c) in your test first")
	}

	snapInfo := snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: snap.R(1)})

	st := s.d.overlord.State()

	st.Lock()
	defer st.Unlock()

	// Put a side info into the state
	snapstate.Set(st, snapInfo.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: snapInfo.SnapName(),
				Revision: snapInfo.Revision,
				SnapID:   "ididid",
			},
		},
		Current:  snapInfo.Revision,
		SnapType: string(snapInfo.Type),
	})

	// Put the snap into the interface repository
	repo := s.d.overlord.InterfaceManager().Repository()
	err := repo.AddSnap(snapInfo)
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

var coreProducerYaml = `
name: core
version: 1
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

var configYaml = `
name: config-snap
version: 1
hooks:
    configure:
`
var aliasYaml = `
name: alias-snap
version: 1
apps:
 app:
 app2:
`

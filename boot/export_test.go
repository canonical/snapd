// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package boot

import (
	"github.com/snapcore/snapd/snap"
)

func NewCoreBootParticipant(s snap.PlaceInfo, t snap.Type, dev Device) *coreBootParticipant {
	bs, err := bootStateFor(t, dev)
	if err != nil {
		panic(err)
	}
	return &coreBootParticipant{s: s, bs: bs}
}

func NewCoreKernel(s snap.PlaceInfo, d Device) *coreKernel {
	return &coreKernel{s, bootloaderOptionsForDeviceKernel(d)}
}

type Trivial = trivial

func (m *Modeenv) WasRead() bool {
	return m.read
}

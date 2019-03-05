// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

// The audio-record interface is the companion interface to the audio-playback
// interface and is not meant to be used without it. The design of this
// interface is based on the idea that the slot implementation (eg pulseaudio)
// is expected to query snapd on if the audio-record slot is connected or not
// and the audio service will mediate recording (ie, the rules below allow
// connecting to the audio service, but do not implement enforcement rules; it
// is up to the audio service to provide enforcement). If other audio recording
// servers require different security policy for record (eg, a different socket
// path), then those accesses will be added to this interface.

const audioRecordSummary = `allows audio recording via supporting services`

const audioRecordBaseDeclarationSlots = `
  audio-record:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
    deny-auto-connection: true
`

const audioRecordConnectedPlugAppArmor = `
# Access for communication with audio recording service done via
# audio-playback interface. The audio service will verify if the audio-record
# interface is connected.
`

type audioRecordInterface struct{}

func (iface *audioRecordInterface) Name() string {
	return "audio-record"
}

func (iface *audioRecordInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              audioRecordSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: audioRecordBaseDeclarationSlots,
	}
}

func (iface *audioRecordInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(audioRecordConnectedPlugAppArmor)
	return nil
}

func (iface *audioRecordInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&audioRecordInterface{})
}

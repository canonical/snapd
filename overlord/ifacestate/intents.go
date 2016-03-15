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

package ifacestate

import (
	"github.com/ubuntu-core/snappy/interfaces"
)

// Intent expresses persistent intent to connect or disconnect certain plug and
// slot.  Intents are a part of the persistent state of the manager.
type Intent struct {
	Action string             `json:"action"`
	Plug   interfaces.PlugRef `json:"plug"`
	Slot   interfaces.SlotRef `json:"slot"`
}

const (
	// IntentConnect represents desire to connect a plug to a slot.
	IntentConnect = "connect"
	// IntentDisconnect represents desire to disconnect a plug from a slot.
	IntentDisconnect = "disconnect"
)

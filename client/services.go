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

package client

import (
	"github.com/snapcore/snapd/systemd"
)

type ServiceOp struct {
	Names  []string `json:"names,omitempty"`
	Action string   `json:"action"`
}

type Service struct {
	Snap    string `json:"snap"`
	AppInfo        // note this is much less than snap.AppInfo, right now
	*systemd.ServiceStatus
	Logs []systemd.Log `json:"logs,omitempty"`
}

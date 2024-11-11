// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/snap"
)

const snapPromptingControlSummary = `allows use of snapd's prompting API and access to prompting-related notice types`

const snapPromptingControlBaseDeclarationPlugs = `
  snap-interfaces-requests-control:
    allow-installation: false
    deny-auto-connection: true
`

const snapPromptingControlBaseDeclarationSlots = `
  snap-interfaces-requests-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type requestsControlInterface struct {
	commonInterface
}

func (iface *requestsControlInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// snaps can declare "handler-service" attribute on the plug, which
	// indicates which user service will be handling prompt requests

	handlerServiceAttr, isSet := plug.Attrs["handler-service"]
	handlerService, ok := handlerServiceAttr.(string)
	if isSet && !ok {
		return fmt.Errorf(`snap-interfaces-requests-control "handler-service" attribute must be a string, not %T %v`,
			handlerServiceAttr, handlerServiceAttr)
	}

	if handlerService == "" {
		return nil
	}

	svc := plug.Snap.Apps[handlerService]
	if svc == nil {
		return fmt.Errorf("declared handler service %q not found in snap", handlerService)
	}

	if !svc.IsService() || svc.DaemonScope != snap.UserDaemon {
		return fmt.Errorf("declared handler service %q is not a user service", handlerService)
	}

	return nil
}

func init() {
	registerIface(&requestsControlInterface{
		commonInterface{
			name:                 "snap-interfaces-requests-control",
			summary:              snapPromptingControlSummary,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			baseDeclarationPlugs: snapPromptingControlBaseDeclarationPlugs,
			baseDeclarationSlots: snapPromptingControlBaseDeclarationSlots,
		},
	})
}

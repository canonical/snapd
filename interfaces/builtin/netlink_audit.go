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

package builtin

import (
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/strutil"
)

const netlinkAuditSummary = `allows access to kernel audit system through netlink`

const netlinkAuditBaseDeclarationSlots = `
  netlink-audit:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const netlinkAuditConnectedPlugSecComp = `
# Description: Can use netlink to read/write to kernel audit system.
bind
socket AF_NETLINK - NETLINK_AUDIT
`

const netlinkAuditConnectedPlugAppArmor = `
# Description: Can use netlink to read/write to kernel audit system.
network netlink,

# CAP_NET_ADMIN required for multicast netlink sockets per 'man 7 netlink'
capability net_admin,

# CAP_AUDIT_READ required to read the audit log via the netlink multicast socket
# per 'man 7 capabilities'
capability audit_read,

# CAP_AUDIT_WRITE required to write to the audit log via the netlink multicast
# socket per 'man 7 capabilities'
capability audit_write,
`

type netlinkAuditInterface struct {
	commonInterface
}

func (iface *netlinkAuditInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		// no apparmor means we don't have to deal with parser features
		return nil
	}
	features := mylog.Check2(apparmor_sandbox.ParserFeatures())

	if !strutil.ListContains(features, "cap-audit-read") {
		// the host system doesn't have the required feature to compile the
		// policy (that happens in 14.04)
		return errors.New("cannot connect plug on system without audit_read support")
	}

	return nil
}

func init() {
	registerIface(&netlinkAuditInterface{commonInterface{
		name:                  "netlink-audit",
		summary:               netlinkAuditSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  netlinkAuditBaseDeclarationSlots,
		connectedPlugSecComp:  netlinkAuditConnectedPlugSecComp,
		connectedPlugAppArmor: netlinkAuditConnectedPlugAppArmor,
	}})
}

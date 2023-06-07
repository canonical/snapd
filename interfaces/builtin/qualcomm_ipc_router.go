// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

/*
 * The AF_QIPCRTR (42) is defined in linux kernel include/linux/socket.h[1]
 * The implementation of this protocol is in net/qrtr[2]
 *
 * [1] https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/include/linux/socket.h
 * [2] https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/net/qrtr
 */
const qipcrtrSummary = `allows access to the Qualcomm IPC Router sockets`

const qipcrtrBaseDeclarationSlots = `
  qualcomm-ipc-router:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
`

const qipcrtrConnectedPlugAppArmorCompat = `
# Description: allows access to the Qualcomm IPC Router sockets
#              and limits to sock_dgram only
network qipcrtr,

# CAP_NET_ADMIN required for port number smaller QRTR_MIN_EPH_SOCKET per 'https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/net/qrtr/qrtr.c'
capability net_admin,
`

const qipcrtrConnectedPlugAppArmor = `
# Description: allows access to the Qualcomm IPC Router sockets
#              and limits to sock_dgram only
network qipcrtr,

unix (connect, send, receive) type=seqpacket addr="@**" peer=(label=###SLOT_SECURITY_TAGS###),
`

const qipcrtrPermanentSlotAppArmor = `
# Description: allows access to the Qualcomm IPC Router sockets
#              and limits to sock_dgram only
network qipcrtr,

# CAP_NET_ADMIN required for port number smaller QRTR_MIN_EPH_SOCKET per 'https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/net/qrtr/qrtr.c'
capability net_admin,
capability net_bind_service,

unix (bind, listen) type=seqpacket addr="@**",
`

const qipcrtrConnectedSlotAppArmor = `
unix (accept, send, receive) type=seqpacket addr="@**" peer=(label=###PLUG_SECURITY_TAGS###),
`

const qipcrtrConnectedPlugSecCompCompat = `
# Description: allows access to the Qualcomm IPC Router sockets
bind

# We allow AF_QIPCRTR in the default template since it is mediated via the AppArmor rule
#socket AF_QIPCRTR
`

const qipcrtrPermanentSlotSecComp = `
# Description: allows access to the Qualcomm IPC Router sockets
bind
accept
accept4
listen

# We allow AF_QIPCRTR in the default template since it is mediated via the AppArmor rule
#socket AF_QIPCRTR
`

type qualcomIPCRouterInterface struct {
	commonInterface
}

func (iface *qualcomIPCRouterInterface) Name() string {
	return "qualcomm-ipc-router"
}

func (iface *qualcomIPCRouterInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              qipcrtrSummary,
		BaseDeclarationSlots: qipcrtrBaseDeclarationSlots,
		// backward compatibility
		ImplicitOnCore: true,
		// backward compatibility
		ImplicitOnClassic: true,
	}
}

func (iface *qualcomIPCRouterInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if implicitSystemConnectedSlot(slot) {
		// backward compatibility
		spec.AddSnippet(qipcrtrConnectedPlugAppArmorCompat)
	} else {
		old := "###SLOT_SECURITY_TAGS###"
		new := slotAppLabelExpr(slot)
		snippet := strings.Replace(qipcrtrConnectedPlugAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *qualcomIPCRouterInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(qipcrtrConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *qualcomIPCRouterInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(qipcrtrPermanentSlotAppArmor)
	return nil
}

func (iface *qualcomIPCRouterInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(qipcrtrPermanentSlotSecComp)
	return nil
}

func (iface *qualcomIPCRouterInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if implicitSystemConnectedSlot(slot) {
		// backward compatibility
		spec.AddSnippet(qipcrtrConnectedPlugSecCompCompat)
	}
	return nil
}

func (iface *qualcomIPCRouterInterface) verifySupport(what string) error {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		// no apparmor means we don't have to deal with parser features
		return nil
	}
	features, err := apparmor_sandbox.ParserFeatures()
	if err != nil {
		return err
	}

	if !strutil.ListContains(features, "qipcrtr-socket") {
		// then the host system doesn't have the required feature to compile the
		// policy, the qipcrtr socket is a new addition not present in i.e.
		// xenial
		return fmt.Errorf("cannot %s on system without qipcrtr socket support", what)
	}

	return nil
}

func (iface *qualcomIPCRouterInterface) BeforePrepareSlot(plug *snap.SlotInfo) error {
	return iface.verifySupport("prepare slot")
}

func (iface *qualcomIPCRouterInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	return iface.verifySupport("connect plug")
}

func init() {
	registerIface(&qualcomIPCRouterInterface{})
}

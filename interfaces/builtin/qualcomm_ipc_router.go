// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/naming"
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
    allow-connection:
      -
        plug-attributes:
          qcipc: $SLOT(qcipc)
      -
        plug-attributes:
          qcipc: $MISSING
        slot-snap-type:
          - core
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
      plug-attributes:
        qcipc: $SLOT(qcipc)
    deny-auto-connection:
      plug-attributes:
        qcipc: $MISSING
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

unix (connect, send, receive) type=seqpacket addr="###SOCKET_ADDRESS###" peer=(label=###SLOT_SECURITY_TAGS###),
`

const qipcrtrPermanentSlotAppArmor = `
# Description: allows access to the Qualcomm IPC Router sockets
#              and limits to sock_dgram only
network qipcrtr,

# CAP_NET_ADMIN required for port number smaller QRTR_MIN_EPH_SOCKET per 'https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/net/qrtr/qrtr.c'
capability net_admin,
capability net_bind_service,

unix (bind, listen) type=seqpacket addr="###SOCKET_ADDRESS###",
`

const qipcrtrConnectedSlotAppArmor = `
unix (accept, send, receive) type=seqpacket addr="###SOCKET_ADDRESS###" peer=(label=###PLUG_SECURITY_TAGS###),
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

const (
	// qcipcAttrib is a tag that must match between plug and slot
	qcipcAttrib = "qcipc"
	// addressAttrib is the allowed socket address and is specified in the slot
	addressAttrib = "address"
)

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

func fillSnippetSocketAddress(slot *interfaces.ConnectedSlot, snippet string) (string, error) {
	var address string
	if err := slot.Attr(addressAttrib, &address); err != nil {
		return "", err
	}

	return strings.ReplaceAll(snippet, "###SOCKET_ADDRESS###", address), nil
}

// Note that we cannot use implicitSystemConnectedSlot as legacy slots can be
// for UC too in this case.
// TODO replace with implicitSystemConnectedSlot when
// https://github.com/snapcore/snapd/pull/13194 lands
func isConnectedSlotSystem(slot *interfaces.ConnectedSlot) bool {
	if slot.Snap().Type() == snap.TypeOS || slot.Snap().Type() == snap.TypeSnapd {
		return true
	}
	return false
}

func isSlotInfoSystem(slot *snap.SlotInfo) bool {
	if slot.Snap.SnapType == snap.TypeOS || slot.Snap.SnapType == snap.TypeSnapd {
		return true
	}
	return false
}

func validateTag(qcipc string) error {
	if len(qcipc) == 0 {
		return fmt.Errorf("qualcomm-ipc-router %s attribute cannot be empty", qcipcAttrib)
	}
	if err := naming.ValidateIfaceTag(qcipc); err != nil {
		return fmt.Errorf("bad name for %s attribute: %v", qcipcAttrib, err)
	}
	return nil
}

func validateAddress(address string) error {
	if len(address) == 0 {
		return fmt.Errorf("qualcomm-ipc-router %s attribute cannot be empty", qcipcAttrib)
	}
	// do not allow apparmor RE in the address
	if err := apparmor.ValidateNoAppArmorRegexp(address); err != nil {
		return fmt.Errorf(`address is invalid: %v`, err)
	}
	return nil
}

func (iface *qualcomIPCRouterInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The qcipc attribute for the plug must be checked for here,
	// as in BeforePreparePlug we do not know yet if we are
	// connecting to a system or app provided slot.
	var qcipc string
	err := plug.Attr(qcipcAttrib, &qcipc)
	if isConnectedSlotSystem(slot) {
		if err == nil {
			return fmt.Errorf("%q attribute not allowed if connecting to a system slot", qcipcAttrib)
		}
		// backward compatibility
		spec.AddSnippet(qipcrtrConnectedPlugAppArmorCompat)
	} else {
		// Plug must have qcipc if connecting to app slot
		if err != nil {
			return err
		}
		if err := validateTag(qcipc); err != nil {
			return err
		}

		old := "###SLOT_SECURITY_TAGS###"
		slotLabel := slot.LabelExpression()
		snippet := strings.ReplaceAll(qipcrtrConnectedPlugAppArmor, old, slotLabel)
		var err error
		if snippet, err = fillSnippetSocketAddress(slot, snippet); err != nil {
			return err
		}
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *qualcomIPCRouterInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if isConnectedSlotSystem(slot) {
		return nil
	}

	old := "###PLUG_SECURITY_TAGS###"
	new := plug.LabelExpression()
	snippet := strings.ReplaceAll(qipcrtrConnectedSlotAppArmor, old, new)
	var err error
	if snippet, err = fillSnippetSocketAddress(slot, snippet); err != nil {
		return err
	}
	spec.AddSnippet(snippet)
	return nil
}

func (iface *qualcomIPCRouterInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if isSlotInfoSystem(slot) {
		return nil
	}

	var address string
	if err := slot.Attr(addressAttrib, &address); err != nil {
		return err
	}
	snippet := strings.ReplaceAll(qipcrtrPermanentSlotAppArmor, "###SOCKET_ADDRESS###", address)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *qualcomIPCRouterInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if isSlotInfoSystem(slot) {
		return nil
	}
	spec.AddSnippet(qipcrtrPermanentSlotSecComp)
	return nil
}

func (iface *qualcomIPCRouterInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if isConnectedSlotSystem(slot) {
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

func (iface *qualcomIPCRouterInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if slot.Attrs == nil {
		slot.Attrs = make(map[string]interface{})
	}
	if isSlotInfoSystem(slot) {
		return nil
	}

	qcipc, ok := slot.Attrs[qcipcAttrib].(string)
	if !ok {
		return fmt.Errorf("qualcomm-ipc-router slot must have a %s attribute", qcipcAttrib)
	}
	if err := validateTag(qcipc); err != nil {
		return err
	}

	address, ok := slot.Attrs[addressAttrib].(string)
	if !ok {
		return fmt.Errorf("qualcomm-ipc-router slot must have an %s attribute", addressAttrib)
	}
	if err := validateAddress(address); err != nil {
		return err
	}

	return iface.verifySupport("prepare slot")
}

func (iface *qualcomIPCRouterInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	return iface.verifySupport("connect plug")
}

func init() {
	registerIface(&qualcomIPCRouterInterface{})
}

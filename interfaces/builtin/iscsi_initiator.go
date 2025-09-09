// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

/*
 * The iscsi-initiator interface allows snaps to act as iSCSI initiators,
 * enabling them to discover, connect to, and manage iSCSI targets for
 * block storage access.
 *
 * The interface loads kernel modules required for iSCSI operations including
 * iscsi-tcp for transport and target-core-mod for LIO functionality.
 */

const iscsiInitiatorSummary = `allows access to iSCSI initiator functionality for block storage operations`

const iscsiInitiatorBaseDeclarationPlugs = `
  iscsi-initiator:
    allow-installation: false
    deny-auto-connection: true
`

const iscsiInitiatorBaseDeclarationSlots = `
  iscsi-initiator:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const iscsiInitiatorConnectedPlugAppArmor = `
# ConfigFS access for Linux-IO (LIO) target management
/sys/kernel/config/target/ rw,
/sys/kernel/config/target/** rw,

# Lock file used by targetcli for exclusive access
/{var/,}run/targetcli.lock rwlk,

# iSCSI initiator configuration files
/etc/iscsi/initiatorname.iscsi r,
/etc/iscsi/iscsid.conf r,
# iSCSI target node information for persistent connections
/etc/iscsi/nodes/ rwk,
/etc/iscsi/nodes/** rw,

# Runtime files and locks for iSCSI daemon operation
/run/lock/iscsi/ rw,
/run/lock/iscsi/** rwlk,
# iSCSI transport layer information (TCP and iSER)
/sys/devices/virtual/iscsi_transport/tcp/** r,
/sys/devices/virtual/iscsi_transport/iser/** r,
# iSCSI session and host management through sysfs
/sys/class/iscsi_session/** rw,
/sys/class/iscsi_host/** r,
# SCSI host adapter information for iSCSI devices
/sys/devices/platform/host*/scsi_host/host*/** rw,
# iSCSI connection state and statistics
/sys/devices/platform/host*/session*/connection*/iscsi_connection/connection*/** rw,
# iSCSI session state and configuration
/sys/devices/platform/host*/session*/iscsi_session/session*/** rw,
# SCSI target device information
/sys/devices/platform/host*/session*/target*/** rw,
# iSCSI host adapter configuration
/sys/devices/platform/host*/iscsi_host/host*/** rw,

# Communication with iscsiadm daemon via abstract Unix socket
unix (send, receive, connect) type=stream peer=(addr="@ISCSIADM_ABSTRACT_NAMESPACE"),
`

type iscsiInitiatorInterface struct {
	commonInterface
}

var iscsiInitiatorConnectedPlugKmod = []string{
	`iscsi-tcp`,       // A module providing iscsi initiator functionality.
	`target-core-mod`, // A module providing ConfigFS infrastructure utilized in LIO (which is used by Cinder for iSCSI targets).
}

func init() {
	registerIface(&iscsiInitiatorInterface{commonInterface{
		name:                     "iscsi-initiator",
		summary:                  iscsiInitiatorSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     iscsiInitiatorBaseDeclarationSlots,
		baseDeclarationPlugs:     iscsiInitiatorBaseDeclarationPlugs,
		connectedPlugAppArmor:    iscsiInitiatorConnectedPlugAppArmor,
		connectedPlugKModModules: iscsiInitiatorConnectedPlugKmod,
	}})
}

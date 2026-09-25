// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/snap/naming"
)

// seclogPeer builds a [seclog.Peer] for AUTHZ events from the
// credentials captured at accept.
func (un *ucrednet) seclogPeer() seclog.Peer {
	// best-effort for logging
	if un == nil {
		return seclog.Peer{
			UID: seclog.PeerNobody,
			PID: seclog.PeerNoProcess,
		}
	}
	peer := seclog.Peer{
		Socket: un.Socket,
		UID:    un.Uid,
		// PIDForPolkit is the accept-time peer pid; using it here is acceptable.
		PID: un.PIDForPolkit,
	}
	// Note that untrusted is just that we have low confidence in the name, not
	// that the process itself is not trusted explicitly.
	if exe, err := un.UntrustedProcessExeName(); err == nil {
		peer.Exe = exe
	}
	if tag, err := un.SecurityTag(); err == nil {
		peer.InstanceName = naming.InstanceName(tag.InstanceName())
		peer.Runnable = seclog.RunnableFromSecurityTag(tag)
	}
	return peer
}

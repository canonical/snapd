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

// This file contains daemon-side helpers for building and emitting security
// audit events via the [seclog] package. Keep seclog integration out of
// daemon.go so new event categories can add helpers here without growing the
// core daemon type.

package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/lsm"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	securityLabelsFromPid = lsm.SecurityLabelsFromPid
	cgroupPathFromPid     = cgroup.ProcessPathInTrackingCgroup
)

// seclogPeerFromUcred builds a [seclog.Peer] for AUTHZ audit events, including
// best-effort enrichment from the peer process.
func seclogPeerFromUcred(ucred *ucrednet) seclog.Peer {
	if ucred == nil {
		return seclog.Peer{
			UID: ucrednetNobody,
			PID: ucrednetNoProcess,
		}
	}
	peer := seclog.Peer{
		Socket: ucred.Socket,
		UID:    ucred.Uid,
		PID:    ucred.Pid,
	}
	return enrichSeclogPeer(peer)
}

func enrichSeclogPeer(peer seclog.Peer) seclog.Peer {
	if peer.PID == ucrednetNoProcess {
		return peer
	}

	pid := int(peer.PID)

	exePath := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/exe", pid))
	if exe, err := osReadlink(exePath); err == nil {
		peer.Exe = exe
	}

	if labels, err := securityLabelsFromPid(pid); err == nil {
		peer.SecurityLabels = labels
	}

	if cgroupPath, err := cgroupPathFromPid(pid); err == nil {
		if tag := cgroup.SecurityTagFromCgroupPath(cgroupPath); tag != nil {
			peer.CgroupLabel = tag.String()
		}
	}

	snap, app := snapAppFromLabel(peer.SecurityLabels[seclog.PeerSecurityLabelAppArmor])
	if snap == "" {
		snap, app = snapAppFromLabel(peer.CgroupLabel)
	}
	peer.Snap = snap
	peer.App = app

	return peer
}

func snapAppFromLabel(label string) (snap, app string) {
	if label == "" {
		return "", ""
	}
	if tag, err := naming.ParseAppSecurityTag(label); err == nil {
		return tag.InstanceName(), tag.AppName()
	}
	if tag, err := naming.ParseHookSecurityTag(label); err == nil {
		return tag.InstanceName(), tag.HookName()
	}
	return "", ""
}

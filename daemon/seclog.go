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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/seclog"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	apparmorSecurityLabelFromPid = apparmor.SecurityLabelFromPid
	cgroupPathFromPid            = cgroup.ProcessPathInTrackingCgroup
)

// seclogPeerFromUcred builds a [seclog.Peer] for AUTHZ audit events, including
// best-effort enrichment from the peer process. AUTHZ emission (p4) is the
// intended caller; this helper is tested via export_test.go until then.
func seclogPeerFromUcred(ucred *ucrednet) seclog.Peer {
	if ucred == nil {
		return seclog.Peer{
			UID: seclog.PeerNobody,
			PID: seclog.PeerNoProcess,
		}
	}
	peer := seclog.Peer{
		Socket: ucred.Socket,
		UID:    ucred.Uid,
		PID:    ucred.Pid,
	}
	return enrichSeclogPeer(peer)
}

// enrichSeclogPeer fills Exe, Snap, and Runnable from /proc. Failures leave
// those fields empty. Snap/runnable prefer the AppArmor tag when it is present
// and not unconfined, otherwise the cgroup tag.
func enrichSeclogPeer(peer seclog.Peer) seclog.Peer {
	if peer.PID == seclog.PeerNoProcess {
		return peer
	}

	pid := int(peer.PID)

	exePath := filepath.Join(dirs.GlobalRootDir, fmt.Sprintf("proc/%d/exe", pid))
	if exe, err := osReadlink(exePath); err == nil {
		peer.Exe = exe
	} else {
		logger.Debugf("cannot read peer %d exe: %v", pid, err)
	}

	var apparmorLabel string
	if label, err := apparmorSecurityLabelFromPid(pid); err == nil {
		apparmorLabel = label
	} else {
		logger.Debugf("cannot read peer %d AppArmor label: %v", pid, err)
	}

	var cgroupTag string
	if cgroupPath, err := cgroupPathFromPid(pid); err == nil {
		if tag := cgroup.SecurityTagFromCgroupPath(cgroupPath); tag != nil {
			cgroupTag = tag.String()
		}
	} else {
		logger.Debugf("cannot read peer %d cgroup path: %v", pid, err)
	}

	if apparmorLabel != "" && apparmorLabel != "unconfined" {
		peer.Snap, peer.Runnable = snapRunnableFromLabel(apparmorLabel)
	} else {
		peer.Snap, peer.Runnable = snapRunnableFromLabel(cgroupTag)
	}

	return peer
}

// snapRunnableFromLabel parses a snap app or hook security tag into instance
// name and runnable. Apps get an "app." prefix; hooks use CommandName as-is.
//
//	snap.firefox.firefox          -> firefox, app.firefox
//	snap.mysnap.hook.install      -> mysnap, hook.install
//	snap.mysnap+comp.hook.install -> mysnap, mysnap+comp.hook.install
func snapRunnableFromLabel(label string) (snap, runnable string) {
	if label == "" {
		return "", ""
	}
	tag, err := naming.ParseSecurityTag(label)
	if err != nil {
		return "", ""
	}
	runnable = tag.CommandName()
	if _, ok := tag.(naming.AppSecurityTag); ok {
		runnable = "app." + runnable
	}
	return tag.InstanceName(), runnable
}

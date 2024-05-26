// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package portal

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
)

const (
	documentPortalBusName    = "org.freedesktop.portal.Documents"
	documentPortalObjectPath = "/org/freedesktop/portal/documents"
	documentPortalIface      = "org.freedesktop.portal.Documents"
)

var (
	userCurrent        = user.Current
	dbusutilSessionBus = dbusutil.SessionBus
)

type Document struct {
	xdgRuntimeDir string
}

// GetUserXdgRuntimeDir returns the runtime directory for the current user.
// TODO: find a better place for this: it could fit well in a generic
// portal.Desktop interface, or even in the "dirs" package (but then we'd lose
// caching, so it needs more thoughts).
func (p *Document) GetUserXdgRuntimeDir() (string, error) {
	if p.xdgRuntimeDir == "" {
		u := mylog.Check2(userCurrent())

		p.xdgRuntimeDir = filepath.Join(dirs.XdgRuntimeDirBase, u.Uid)
	}
	return p.xdgRuntimeDir, nil
}

func (p *Document) GetDefaultMountPoint() (string, error) {
	xdgRuntimeDir := mylog.Check2(p.GetUserXdgRuntimeDir())

	return filepath.Join(xdgRuntimeDir, "doc"), nil
}

func (p *Document) GetMountPoint() (string, error) {
	conn := mylog.Check2(dbusutilSessionBus())

	portal := conn.Object(documentPortalBusName, documentPortalObjectPath)
	var mountPoint []byte
	mylog.Check(portal.Call(documentPortalIface+".GetMountPoint", 0).Store(&mountPoint))

	return strings.TrimRight(string(mountPoint), "\x00"), nil
}

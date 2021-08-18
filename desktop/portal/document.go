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
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

const (
	documentPortalBusName    = "org.freedesktop.portal.Documents"
	documentPortalObjectPath = "/org/freedesktop/portal/documents"
	documentPortalIface      = "org.freedesktop.portal.Documents"
)

var (
	userCurrent     = user.Current
	osGetenv        = os.Getenv
	osutilIsMounted = osutil.IsMounted
)

type Document struct {
	xdgRuntimeDir string
}

func (p *Document) getUserXdgRuntimeDir() (string, error) {
	if p.xdgRuntimeDir == "" {
		u, err := userCurrent()
		if err != nil {
			return "", fmt.Errorf(i18n.G("cannot get the current user: %s"), err)
		}
		p.xdgRuntimeDir = filepath.Join(dirs.XdgRuntimeDirBase, u.Uid)
	}
	return p.xdgRuntimeDir, nil
}

func (p *Document) GetDefaultMountPoint() (string, error) {
	xdgRuntimeDir, err := p.getUserXdgRuntimeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(xdgRuntimeDir, "doc"), nil
}

func (p *Document) Activate() error {
	expectedMountPoint, err := p.GetDefaultMountPoint()
	if err != nil {
		return err
	}

	// If $XDG_RUNTIME_DIR/doc appears to be a mount point, assume
	// that the document portal is up and running.
	if mounted, err := osutilIsMounted(expectedMountPoint); err != nil {
		logger.Noticef("Could not check document portal mount state: %s", err)
	} else if mounted {
		return nil
	}

	// If there is no session bus, our job is done.  We check this
	// manually to avoid dbus.SessionBus() auto-launching a new
	// bus.
	busAddress := osGetenv("DBUS_SESSION_BUS_ADDRESS")
	if len(busAddress) == 0 {
		return nil
	}

	// We've previously tried to start the document portal and
	// were told the service is unknown: don't bother connecting
	// to the session bus again.
	//
	// As the file is in $XDG_RUNTIME_DIR, it will be cleared over
	// full logout/login or reboot cycles.
	xdgRuntimeDir, err := p.getUserXdgRuntimeDir()
	if err != nil {
		return err
	}

	portalsUnavailableFile := filepath.Join(xdgRuntimeDir, ".portals-unavailable")
	if osutil.FileExists(portalsUnavailableFile) {
		return nil
	}

	conn, err := dbusutil.SessionBus()
	if err != nil {
		return err
	}

	portal := conn.Object(documentPortalBusName, documentPortalObjectPath)
	var mountPoint []byte
	if err := portal.Call(documentPortalIface+".GetMountPoint", 0).Store(&mountPoint); err != nil {
		// It is not considered an error if
		// xdg-document-portal is not available on the system.
		if dbusErr, ok := err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
			// We ignore errors here: if writing the file
			// fails, we'll just try connecting to D-Bus
			// again next time.
			if err = ioutil.WriteFile(portalsUnavailableFile, []byte(""), 0644); err != nil {
				logger.Noticef("WARNING: cannot write file at %s: %s", portalsUnavailableFile, err)
			}
			return nil
		}
		return err
	}

	// Sanity check to make sure the document portal is exposed
	// where we think it is.
	actualMountPoint := strings.TrimRight(string(mountPoint), "\x00")
	if actualMountPoint != expectedMountPoint {
		return fmt.Errorf(i18n.G("Expected portal at %#v, got %#v"), expectedMountPoint, actualMountPoint)
	}
	return nil
}

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

package userd

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"gopkg.in/ini.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/systemd"
)

type dbusInterface interface {
	Interface() string
	ObjectPath() dbus.ObjectPath
	IntrospectionData() string
}

type Userd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []dbusInterface
}

// userdBusNames contains the list of bus names userd will acquire on
// the session bus.  It is unnecessary (and undesirable) to add more
// names here when adding new interfaces to the daemon.
var userdBusNames = []string{
	"io.snapcraft.Launcher",
	"io.snapcraft.Settings",
}

func clearBrokenLink(targetPath string) error {
	_, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		switch err.(type) {
		case *fs.PathError:
			logger.Noticef("deleting broken link in snap service %s", targetPath)
			// it's a broken link; delete it
			os.Remove(targetPath)
		default:
			return err
		}
	}
	return nil
}

func reenableUserService(serviceName string) error {
	logger.Noticef("re-enabling user service %s", serviceName)
	sysd := systemd.New(systemd.UserMode, nil)
	return sysd.DaemonReEnable([]string{serviceName})
}

func checkServicePlacement(targetName, snapTarget string) error {
	snapTargetData, err := os.ReadFile(snapTarget)
	if err != nil {
		return err
	}
	targetIni, err := ini.Load(snapTargetData)
	if err != nil {
		return err
	}
	wantedBy := ""
	if section := targetIni.Section("Install"); section != nil {
		wantedBy = section.Key("WantedBy").String()
	}
	if wantedBy != targetName {
		// the symlink is in the wrong folder. Re-enable the service
		// to change it.
		err := reenableUserService(path.Base(snapTarget))
		if err != nil {
			return err
		}
	}
	return nil
}

func sanitizeUserServices() error {
	// ensure that the user services enabled in the $HOME folder are
	// correctly placed in the right .target.wants folder. This placement
	// will be wrong if a service is moved from default.target to
	// graphical-session.target or vice-versa, so this clean up is required.
	//
	// The change can happen in two cases:
	//
	// * when migrating from an old version of snapd without "graphical-session.target"
	//   support, to a new version that supports it: all the user daemons' WantedBy entry
	//   will be updated, and so these entries will have to be updated too.
	// * when a snap adds or removes the `desktop` plug in a daemon

	userSystemdQuery := path.Join(os.Getenv("HOME"), ".config", "systemd", "user", "*.target.wants")
	entries, err := filepath.Glob(userSystemdQuery)
	if err != nil {
		return err
	}
	for _, targetWantPath := range entries {
		snapTargetPaths, err := filepath.Glob(path.Join(targetWantPath, "snap.*"))
		if err != nil {
			logger.Noticef("cannot get the user service files at %s: %s", targetWantPath, err.Error())
			continue
		}
		for _, snapTarget := range snapTargetPaths {
			// Remove any broken link (that's a service that was removed)
			if err = clearBrokenLink(snapTarget); err != nil {
				logger.Noticef("cannot remove the broken link %s: %s", snapTarget, err.Error())
				continue
			}
			// Check that any snap service is in the right .target.wants
			targetName := strings.TrimSuffix(targetWantPath, ".wants")
			if err := checkServicePlacement(path.Base(targetName), snapTarget); err != nil {
				logger.Noticef("cannot process user service at %s for %s: %s", snapTarget, targetName, err.Error())
				continue
			}
		}
	}
	return nil
}

func (ud *Userd) Init() error {
	var err error

	ud.conn, err = dbusutil.SessionBusPrivate()
	if err != nil {
		return err
	}

	ud.dbusIfaces = []dbusInterface{
		&Launcher{ud.conn},
		&PrivilegedDesktopLauncher{ud.conn},
		&Settings{ud.conn},
	}
	for _, iface := range ud.dbusIfaces {
		// export the interfaces at the godbus API level first to avoid
		// the race between being able to handle a call to an interface
		// at the object level and the actual well-known object name
		// becoming available on the bus
		xml := "<node>" + iface.IntrospectionData() + introspect.IntrospectDataString + "</node>"
		ud.conn.Export(iface, iface.ObjectPath(), iface.Interface())
		ud.conn.Export(introspect.Introspectable(xml), iface.ObjectPath(), "org.freedesktop.DBus.Introspectable")

	}

	for _, name := range userdBusNames {
		// beyond this point the name is available and all handlers must
		// have been set up
		reply, err := ud.conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil {
			return err
		}

		if reply != dbus.RequestNameReplyPrimaryOwner {
			return fmt.Errorf("cannot obtain bus name '%s'", name)
		}
	}

	sanitizeUserServices()
	return nil
}

func (ud *Userd) Start() {
	logger.Noticef("Starting snap userd")

	ud.tomb.Go(func() error {
		// Listen to keep our thread up and running. All DBus bits
		// are running in the background
		<-ud.tomb.Dying()
		ud.conn.Close()

		err := ud.tomb.Err()
		if err != nil && err != tomb.ErrStillAlive {
			return err
		}
		return nil
	})
}

func (ud *Userd) Stop() error {
	ud.tomb.Kill(nil)
	return ud.tomb.Wait()
}

func (ud *Userd) Dying() <-chan struct{} {
	return ud.tomb.Dying()
}

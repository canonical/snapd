// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func ClientAppInfoNotes(app *client.AppInfo) string {
	if !app.IsService() {
		return "-"
	}

	var notes = make([]string, 0, 2)
	var seenTimer, seenSocket bool
	for _, act := range app.Activators {
		switch act.Type {
		case "timer":
			seenTimer = true
		case "socket":
			seenSocket = true
		}
	}
	if seenTimer {
		notes = append(notes, "timer-activated")
	}
	if seenSocket {
		notes = append(notes, "socket-activated")
	}
	if len(notes) == 0 {
		return "-"
	}
	return strings.Join(notes, ",")
}

func ClientAppInfosFromSnapAppInfos(apps []*snap.AppInfo) ([]client.AppInfo, error) {
	// TODO: pass in an actual notifier here instead of null
	//       (Status doesn't _need_ it, but benefits from it)
	sysd := systemd.New(dirs.GlobalRootDir, progress.Null)

	out := make([]client.AppInfo, 0, len(apps))
	for _, app := range apps {
		appInfo := client.AppInfo{
			Snap:     app.Snap.InstanceName(),
			Name:     app.Name,
			CommonID: app.CommonID,
		}
		if fn := app.DesktopFile(); osutil.FileExists(fn) {
			appInfo.DesktopFile = fn
		}

		appInfo.Daemon = app.Daemon
		if !app.IsService() || !app.Snap.IsActive() {
			out = append(out, appInfo)
			continue
		}

		// collect all services for a single call to systemctl
		serviceNames := make([]string, 0, 1+len(app.Sockets)+1)
		serviceNames = append(serviceNames, app.ServiceName())

		sockSvcFileToName := make(map[string]string, len(app.Sockets))
		for _, sock := range app.Sockets {
			sockUnit := filepath.Base(sock.File())
			sockSvcFileToName[sockUnit] = sock.Name
			serviceNames = append(serviceNames, sockUnit)
		}
		if app.Timer != nil {
			timerUnit := filepath.Base(app.Timer.File())
			serviceNames = append(serviceNames, timerUnit)
		}

		// sysd.Status() makes sure that we get only the units we asked
		// for and raises an error otherwise
		sts, err := sysd.Status(serviceNames...)
		if err != nil {
			return nil, fmt.Errorf("cannot get status of services of app %q: %v", app.Name, err)
		}
		if len(sts) != len(serviceNames) {
			return nil, fmt.Errorf("cannot get status of services of app %q: expected %v results, got %v", app.Name, len(serviceNames), len(sts))
		}
		for _, st := range sts {
			switch filepath.Ext(st.UnitName) {
			case ".service":
				appInfo.Enabled = st.Enabled
				appInfo.Active = st.Active
			case ".timer":
				appInfo.Activators = append(appInfo.Activators, client.AppActivator{
					Name:    app.Name,
					Enabled: st.Enabled,
					Active:  st.Active,
					Type:    "timer",
				})
			case ".socket":
				appInfo.Activators = append(appInfo.Activators, client.AppActivator{
					Name:    sockSvcFileToName[st.UnitName],
					Enabled: st.Enabled,
					Active:  st.Active,
					Type:    "socket",
				})
			}
		}
		out = append(out, appInfo)
	}

	return out, nil
}

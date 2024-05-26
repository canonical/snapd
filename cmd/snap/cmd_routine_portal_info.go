// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"fmt"
	"path/filepath"
	"text/template"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type cmdRoutinePortalInfo struct {
	clientMixin
	PortalInfoOptions struct {
		Pid int
	} `positional-args:"true" required:"true"`
}

var (
	shortRoutinePortalInfoHelp = i18n.G("Return information about a process")
	longRoutinePortalInfoHelp  = i18n.G(`
The portal-info command returns information about a process in keyfile format.

This command is used by the xdg-desktop-portal service to retrieve
information about snap confined processes.
`)
)

func init() {
	addRoutineCommand("portal-info", shortRoutinePortalInfoHelp, longRoutinePortalInfoHelp, func() flags.Commander {
		return &cmdRoutinePortalInfo{}
	}, nil, []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<process ID>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Process ID of confined app"),
	}})
}

var (
	cgroupSnapNameFromPid  = cgroup.SnapNameFromPid
	apparmorSnapAppFromPid = apparmor.SnapAppFromPid
)

func (x *cmdRoutinePortalInfo) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := mylog.Check2(cgroupSnapNameFromPid(x.PortalInfoOptions.Pid))

	snap, _ := mylog.Check3(x.client.Snap(snapName))

	// Try to identify the application name from AppArmor
	var app *client.AppInfo
	if snapName, appName, _ := mylog.Check4(apparmorSnapAppFromPid(x.PortalInfoOptions.Pid)); err == nil && snapName == snap.Name && appName != "" {
		for i := range snap.Apps {
			if snap.Apps[i].Name == appName {
				app = &snap.Apps[i]
				break
			}
		}
	}
	// As a fallback, pick an app with a desktop file, favouring
	// the app named identically to the snap.
	if app == nil {
		for i := range snap.Apps {
			if snap.Apps[i].DesktopFile != "" && (app == nil || snap.Apps[i].Name == snap.Name) {
				app = &snap.Apps[i]
			}
		}
	}

	var desktopFile string
	if app != nil {
		desktopFile = filepath.Base(app.DesktopFile)
	}

	var commonID string
	if app != nil {
		commonID = app.CommonID
	}

	// Determine whether the snap has access to the network status
	// TODO: use direct API for asking about interface being connected if
	// that becomes available
	connections := mylog.Check2(x.client.Connections(&client.ConnectionOptions{
		Snap:      snap.Name,
		Interface: "network-status",
	}))

	// XXX: on non-AppArmor systems, or systems where there is only a
	// partial AppArmor support, the snap may still be able to access the
	// network despite the 'network' interface being disconnected
	var hasNetworkStatus bool
	for _, conn := range connections.Established {
		if conn.Plug.Snap == snap.Name && conn.Interface == "network-status" {
			hasNetworkStatus = true
			break
		}
	}

	const portalInfoTemplate = `[Snap Info]
InstanceName={{.Snap.Name}}
{{- if .App}}
AppName={{.App.Name}}
{{- end}}
{{- if .DesktopFile}}
DesktopFile={{.DesktopFile}}
{{- end}}
{{- if .CommonID}}
CommonID={{.CommonID}}
{{- end}}
HasNetworkStatus={{.HasNetworkStatus}}
`
	t := template.Must(template.New("portal-info").Parse(portalInfoTemplate))
	data := struct {
		Snap             *client.Snap
		App              *client.AppInfo
		DesktopFile      string
		CommonID         string
		HasNetworkStatus bool
	}{
		Snap:             snap,
		App:              app,
		DesktopFile:      desktopFile,
		CommonID:         commonID,
		HasNetworkStatus: hasNetworkStatus,
	}
	mylog.Check(t.Execute(Stdout, data))

	return nil
}

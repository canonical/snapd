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
	"text/template"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
)

type cmdRoutinePortalInfo struct {
	clientMixin
	PortalInfoOptions struct {
		Pid int
	} `positional-args:"true" required:"true"`
}

var shortRoutinePortalInfoHelp = i18n.G("Return information about a process")
var longRoutinePortalInfoHelp = i18n.G(`
The portal-info command returns information about a process in keyfile format.

This command is used by the xdg-desktop-portal service to retrieve
information about snap confined processes.
`)

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

const portalInfoTemplate = `[Snap Info]
InstanceName={{.Snap.Name}}
{{- if .App}}
AppName={{.App.Name}}
{{- if .App.DesktopFile}}
DesktopFile={{.App.DesktopFile}}
{{- end}}
{{- end}}
`

var snapNameFromPid = snap.NameFromPid

func (x *cmdRoutinePortalInfo) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	procInfo, err := snapNameFromPid(x.PortalInfoOptions.Pid)
	if err != nil {
		return err
	}
	snap, _, err := x.client.Snap(procInfo.InstanceName)
	if err != nil {
		return err
	}

	// If we were able to identify the application for the pid, use that.
	var app *client.AppInfo
	if procInfo.AppName != "" {
		for i := range snap.Apps {
			if snap.Apps[i].Name == procInfo.AppName {
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

	t := template.Must(template.New("portal-info").Parse(portalInfoTemplate))
	data := struct {
		Snap *client.Snap
		App  *client.AppInfo
	}{
		Snap: snap,
		App:  app,
	}
	return t.Execute(Stdout, data)
	return nil
}

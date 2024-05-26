// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

// Package clientutil offers utilities to turn snap.Info and related
// structs into client structs and to work with the latter.
package clientutil

import (
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// A StatusDecorator is able to decorate client.AppInfos with service status.
type StatusDecorator interface {
	DecorateWithStatus(appInfo *client.AppInfo, snapApp *snap.AppInfo) error
}

// ClientSnapFromSnapInfo returns a client.Snap derived from snap.Info.
// If an optional StatusDecorator is provided it will be used to
// add service status information.
func ClientSnapFromSnapInfo(snapInfo *snap.Info, decorator StatusDecorator) (*client.Snap, error) {
	var publisher *snap.StoreAccount
	if snapInfo.Publisher.Username != "" {
		publisher = &snapInfo.Publisher
	}

	confinement := snapInfo.Confinement
	if confinement == "" {
		confinement = snap.StrictConfinement
	}

	snapapps := make([]*snap.AppInfo, 0, len(snapInfo.Apps))
	for _, app := range snapInfo.Apps {
		snapapps = append(snapapps, app)
	}
	sort.Sort(snap.AppInfoBySnapApp(snapapps))

	apps := mylog.Check2(ClientAppInfosFromSnapAppInfos(snapapps, decorator))
	result := &client.Snap{
		Description: snapInfo.Description(),
		Developer:   snapInfo.Publisher.Username,
		Publisher:   publisher,
		Icon:        snapInfo.Media.IconURL(),
		ID:          snapInfo.ID(),
		InstallDate: snapInfo.InstallDate(),
		Name:        snapInfo.InstanceName(),
		Revision:    snapInfo.Revision,
		Summary:     snapInfo.Summary(),
		Type:        string(snapInfo.Type()),
		Base:        snapInfo.Base,
		Version:     snapInfo.Version,
		Channel:     snapInfo.Channel,
		Private:     snapInfo.Private,
		Confinement: string(confinement),
		Apps:        apps,
		Broken:      snapInfo.Broken,
		Title:       snapInfo.Title(),
		License:     snapInfo.License,
		Media:       snapInfo.Media,
		Prices:      snapInfo.Prices,
		Channels:    snapInfo.Channels,
		Tracks:      snapInfo.Tracks,
		CommonIDs:   snapInfo.CommonIDs,
		Links:       snapInfo.Links(),
		Contact:     snapInfo.Contact(),
		Website:     snapInfo.Website(),
		StoreURL:    snapInfo.StoreURL,
		Categories:  snapInfo.Categories,
	}

	return result, err
}

func ClientAppInfoNotes(app *client.AppInfo) string {
	if !app.IsService() {
		return "-"
	}

	notes := make([]string, 0, 4)
	if app.DaemonScope == snap.UserDaemon {
		notes = append(notes, "user")
	}
	var seenTimer, seenSocket, seenDbus bool
	for _, act := range app.Activators {
		switch act.Type {
		case "timer":
			seenTimer = true
		case "socket":
			seenSocket = true
		case "dbus":
			seenDbus = true
		}
	}
	if seenTimer {
		notes = append(notes, "timer-activated")
	}
	if seenSocket {
		notes = append(notes, "socket-activated")
	}
	if seenDbus {
		notes = append(notes, "dbus-activated")
	}
	if len(notes) == 0 {
		return "-"
	}
	return strings.Join(notes, ",")
}

// ClientAppInfosFromSnapAppInfos returns client.AppInfos derived from
// the given snap.AppInfos.
// If an optional StatusDecorator is provided it will be used to add
// service status information as well, this will be done only if the
// snap is active and when the app is a service.
func ClientAppInfosFromSnapAppInfos(apps []*snap.AppInfo, decorator StatusDecorator) ([]client.AppInfo, error) {
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
		appInfo.DaemonScope = app.DaemonScope
		if !app.IsService() || decorator == nil || !app.Snap.IsActive() {
			out = append(out, appInfo)
			continue
		}
		mylog.Check(decorator.DecorateWithStatus(&appInfo, app))

		out = append(out, appInfo)
	}

	return out, nil
}

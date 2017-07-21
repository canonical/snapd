// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package client

import (
	"errors"
	"net/url"
	"strings"
)

type AppInfo struct {
	Snap         string `json:"snap,omitempty"`
	Name         string `json:"name"`
	DesktopFile  string `json:"desktop-file,omitempty"`
	*ServiceInfo `json:",omitempty"`
}

// IsService returns true if the application is a background daemon.
func (a *AppInfo) IsService() bool {
	if a == nil {
		return false
	}
	if a.ServiceInfo == nil {
		return false
	}
	if a.ServiceInfo.Daemon == "" {
		return false
	}

	return true
}

type ServiceInfo struct {
	Daemon          string `json:"daemon"`
	ServiceFileName string `json:"service-file-name"`
	Enabled         bool   `json:"enabled"`
	Active          bool   `json:"active"`
}

// AppInfoWanted can be used to specify what kind of app is of
// interest.
type AppInfoWanted struct {
	Services bool
	Commands bool
}

// AppInfos returns information about all matching apps. Each name can
// be either a snap or a snap.app. If names is empty, list all.
//
// Use wanted to include apps of the given kind; nil means all.
func (client *Client) AppInfos(names []string, wanted *AppInfoWanted) ([]*AppInfo, error) {
	q := make(url.Values)
	if len(names) > 0 {
		q.Add("apps", strings.Join(names, ","))
	}
	if wanted != nil && !(wanted.Services && wanted.Commands) {
		if !wanted.Services && !wanted.Commands {
			return nil, errors.New("cannot pass in an empty AppInfoWanted")
		}
		wantedStr := "services"
		if wanted.Commands {
			wantedStr = "commands"
		}
		q.Add("wanted", wantedStr)
	}

	var appInfos []*AppInfo
	_, err := client.doSync("GET", "/v2/apps", q, nil, nil, &appInfos)

	return appInfos, err
}

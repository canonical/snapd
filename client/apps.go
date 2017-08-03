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
	"net/url"
	"strings"
)

// AppInfo describes a single snap application.
type AppInfo struct {
	Snap        string `json:"snap,omitempty"`
	Name        string `json:"name"`
	DesktopFile string `json:"desktop-file,omitempty"`
	Daemon      string `json:"daemon,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

// IsService returns true if the application is a background daemon.
func (a *AppInfo) IsService() bool {
	if a == nil {
		return false
	}
	if a.Daemon == "" {
		return false
	}

	return true
}

// AppOptions represent the options of the Apps call.
type AppOptions struct {
	// If Service is true, only return apps that are services
	// (app.IsService() is true); otherwise, return all.
	Service bool
}

// Apps returns information about all matching apps. Each name can be
// either a snap or a snap.app. If names is empty, list all (that
// satisfy opts).
func (client *Client) Apps(names []string, opts AppOptions) ([]*AppInfo, error) {
	q := make(url.Values)
	if len(names) > 0 {
		q.Add("names", strings.Join(names, ","))
	}
	if opts.Service {
		q.Add("select", "service")
	}

	var appInfos []*AppInfo
	_, err := client.doSync("GET", "/v2/apps", q, nil, nil, &appInfos)

	return appInfos, err
}

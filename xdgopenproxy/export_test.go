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

package xdgopenproxy

import (
	"time"
)

type (
	DesktopLauncher = desktopLauncher
	PortalLauncher  = portalLauncher
	UserdLauncher   = userdLauncher

	ResponseError = responseError
)

const (
	DesktopPortalBusName      = desktopPortalBusName
	DesktopPortalObjectPath   = desktopPortalObjectPath
	DesktopPortalOpenURIIface = desktopPortalOpenURIIface
	DesktopPortalRequestIface = desktopPortalRequestIface

	UserdLauncherBusName    = userdLauncherBusName
	UserdLauncherObjectPath = userdLauncherObjectPath
	UserdLauncherIface      = userdLauncherIface
)

var (
	Launch        = launch
	LaunchWithOne = launchWithOne
)

func MakeResponseError(msg string) error {
	return &responseError{msg: msg}
}

func MockPortalTimeout(t time.Duration) (restore func()) {
	old := defaultPortalRequestTimeout
	defaultPortalRequestTimeout = t
	return func() {
		defaultPortalRequestTimeout = old
	}
}

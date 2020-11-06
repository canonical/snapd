// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package daemon

import (
	"net/http"
	"path/filepath"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/secboot"
)

var systemRecoveryKeysCmd = &Command{
	Path:     "/v2/system-recovery-keys",
	GET:      getSystemRecoveryKeys,
	RootOnly: true,
}

func getSystemRecoveryKeys(c *Command, r *http.Request, user *auth.UserState) Response {
	var rsp client.SystemRecoveryKeysResponse

	rkey, err := secboot.RecoveryKeyFromFile(filepath.Join(dirs.SnapFDEDir, "recovery.key"))
	if err != nil {
		return InternalError(err.Error())
	}
	rsp.RecoveryKey = rkey.String()

	reinstallKey, err := secboot.RecoveryKeyFromFile(filepath.Join(dirs.SnapFDEDir, "reinstall.key"))
	if err != nil {
		return InternalError(err.Error())
	}
	rsp.ReinstallKey = reinstallKey.String()

	return SyncResponse(&rsp, nil)
}

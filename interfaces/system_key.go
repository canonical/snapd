// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var systemKeyInputs []string

func init() {
	buildID, err := osutil.MyBuildID()
	if err != nil {
		logger.Noticef("cannot get builID: %s", err)
		return
	}
	systemKeyInputs = append(systemKeyInputs, fmt.Sprintf("build-id: %s", buildID))

	// FIXME: add $( ls /sys/kernel/security/apparmor/features) output
	// FIXME2: make this yaml
	// FIXME3: do not make it a hash
}

// SystemKey outputs a digest that uniquely identifies what security
// profiles this snapd understands. Everytime there is an incompatible
// change in any of snapds format this digest will change. Later more
// inputs (like what kernel version etc) may be added.
func SystemKey() string {
	h := md5.New()
	for _, s := range systemKeyInputs {
		h.Write([]byte(s))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// -*- Mode: Go; indent-tabs-mode: t -*-
// +build withsecboot

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

package secboot

import (
	"fmt"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
)

func CheckKeySealingSupported() error {
	logger.Noticef("checking TPM device availability...")
	tconn, err := sb.ConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM device: %v", err)
	}
	logger.Noticef("TPM device detected")
	return tconn.Close()
}

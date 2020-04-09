// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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
	"errors"
	"fmt"
	"os"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
)

var secbootConnectToDefaultTPM = sb.ConnectToDefaultTPM

func CheckKeySealingSupported() error {
	logger.Noticef("checking if secure boot is enabled...")
	if err := checkSecureBootEnabled(); err != nil {
		return err
	}
	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tconn, err := secbootConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM device: %v", err)
	}
	logger.Noticef("TPM device detected")
	return tconn.Close()
}

var efivarsSecureBootFile = "/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"

func checkSecureBootEnabled() error {
	f, err := os.Open(efivarsSecureBootFile)
	if err != nil {
		return fmt.Errorf("cannot open secure boot file: %v", err)
	}
	defer f.Close()

	buf := make([]uint8, 5)
	_, err = f.Read(buf)
	if err != nil {
		return fmt.Errorf("cannot read secure boot file: %v", err)
	}
	if buf[4] != 1 {
		return errors.New("secure boot is disabled")
	}

	return nil
}

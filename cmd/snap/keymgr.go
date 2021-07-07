// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type KeypairManager interface {
	asserts.KeypairManager

	GetByName(keyNname string) (asserts.PrivateKey, error)
	Export(keyName string) ([]byte, error)
	List() ([]asserts.ExternalKeyInfo, error)
}

func getKeypairManager() (KeypairManager, error) {
	keymgrPath := os.Getenv("SNAPD_EXT_KEYMGR")
	if keymgrPath != "" {
		keypairMgr, err := asserts.NewExternalKeypairManager(keymgrPath)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("cannot setup external keypair manager: %v"), err)
		}
		return keypairMgr, nil
	}
	keypairMgr := asserts.NewGPGKeypairManager()
	return keypairMgr, nil
}

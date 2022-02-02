// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot
// +build nosecboot

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

package secboot

import (
	"errors"

	"github.com/snapcore/snapd/kernel/fde"
)

var errBuildWithoutSecboot = errors.New("build without secboot support")

func CheckTPMKeySealingSupported() error {
	return errBuildWithoutSecboot
}

func SealKeys(keys []SealKeyRequest, params *SealKeysParams) error {
	return errBuildWithoutSecboot
}

func SealKeysWithFDESetupHook(runHook fde.RunSetupHookFunc, keys []SealKeyRequest, params *SealKeysWithFDESetupHookParams) error {
	return errBuildWithoutSecboot
}

func ResealKeys(params *ResealKeysParams) error {
	return errBuildWithoutSecboot
}

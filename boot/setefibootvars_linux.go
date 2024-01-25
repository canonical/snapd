// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package boot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/canonical/go-efilib"
	"github.com/canonical/go-efilib/linux"

	"github.com/snapcore/snapd/bootloader"
)

var (
	ErrAllBootNumsUsed    = errors.New("all Boot#### variable numbers are already in use")
	ErrNoMatchingVariable = errors.New("no variable matches the given boot option")
	ErrInvalidBootOrder   = errors.New("BootOrder variable data must have even length")

	defaultVarAttrs = efi.AttributeNonVolatile | efi.AttributeBootserviceAccess | efi.AttributeRuntimeAccess

	efiListVariables = efi.ListVariables
	efiReadVariable  = efi.ReadVariable
	efiWriteVariable = efi.WriteVariable

	linuxFilePathToDevicePath = linux.FilePathToDevicePath

	bootOptionRegexp = regexp.MustCompile("^Boot[0-9A-F]{4}$")
)

// constructLoadOption returns a serialized EFI load option with the device
// path corresponding to the given asset path, along with the given description
// and optional data.
func constructLoadOption(description string, assetPath string, optionalData []byte) ([]byte, error) {
	devicePath, err := linuxFilePathToDevicePath(assetPath, linux.ShortFormPathHD)
	if err != nil {
		return nil, err
	}
	loadOption := &efi.LoadOption{
		Attributes:   efi.LoadOptionActive | efi.LoadOptionCategoryBoot,
		Description:  description,
		FilePath:     devicePath,
		OptionalData: optionalData,
	}
	loadOptionSerialized, err := loadOption.Bytes()
	if err != nil {
		return nil, err
	}
	return loadOptionSerialized, nil
}

// findMatchingBootOption searches existing Boot#### variables for one
// which matches the given data.
//
// If there is a match, returns the boot number of the existing variable.
// Otherwise, finds the first available boot number and returns it, along with
// ErrNoMatchingVariable, indicating that a new boot option with that boot
// number should be written. If a different error occurs, returns that error,
// and the returned boot number should be ignored.
func findMatchingBootOption(optionData []byte) (uint16, error) {
	variables, err := efiListVariables()
	if err != nil {
		return 0, err
	}
	usedBootNums := make(map[uint64]bool)
	for _, varDesc := range variables {
		varName := varDesc.Name
		varGUID := varDesc.GUID
		if !bootOptionRegexp.MatchString(varName) {
			// Not a Boot#### variable, so skip it
			continue
		}
		if varGUID != efi.GlobalVariable {
			// Not an EFI global variable, so skip it
			continue
		}
		varNumber, err := strconv.ParseUint(varName[4:], 16, 16)
		if err != nil {
			// Should not occur, since variable matched bootOptionRegexp
			return 0, err
		}
		// Since we never overwrite an existing variable, we can ignore
		// variable attributes when reading the variable
		varData, _, err := efiReadVariable(varName, varGUID)
		if err != nil {
			return 0, err
		}
		if bytes.Compare(optionData, varData) == 0 {
			// existing variable already identical, use it
			return uint16(varNumber), nil
		}
		usedBootNums[varNumber] = true
	}
	for bootNum := uint64(0); bootNum <= 0xFFFF; bootNum++ {
		if !usedBootNums[bootNum] {
			return uint16(bootNum), ErrNoMatchingVariable
		}
	}
	return 0, ErrAllBootNumsUsed
}

// setEfiBootOptionVariable ensures that a Boot#### variable contains
// the given EFI load option.
//
// It may be the case that an existing boot variable already contains the
// given load option, in which case that boot variable is reused. Otherwise,
// finds the first unused boot variable number and uses it. Writes the load
// option to that variable, and returns the number of the variable that was
// used.
func setEfiBootOptionVariable(loadOptionData []byte) (uint16, error) {
	bootNum, err := findMatchingBootOption(loadOptionData)
	if err == nil {
		return bootNum, nil
	} else if err != ErrNoMatchingVariable {
		return 0, err
	}
	varName := fmt.Sprintf("Boot%04X", bootNum)
	err = efiWriteVariable(varName, efi.GlobalVariable, defaultVarAttrs, loadOptionData)
	return bootNum, err
}

// setEfiBootOrderVariable reads the current BootOrder variable,
// inserts the given newBootNum at the beginning of the number list
// (and removes it from later in the list if it occurs) and writes the
// list as the new BootOrder variable.
func setEfiBootOrderVariable(newBootNum uint16) error {
	origData, attrs, err := efiReadVariable("BootOrder", efi.GlobalVariable)
	if err == efi.ErrVarNotExist {
		attrs = defaultVarAttrs
		origData = make([]byte, 0)
	} else if err != nil {
		return err
	}
	if len(origData)%2 != 0 {
		return ErrInvalidBootOrder
	}
	var optionOffset = -1
	for i := 0; i < len(origData); i += 2 {
		bootNum := binary.LittleEndian.Uint16(origData[i : i+2])
		if newBootNum == bootNum {
			optionOffset = i
			break
		}
	}
	var newData []byte
	if optionOffset == 0 {
		// newBootNum already at start, no need to re-write variable
		return nil
	} else if optionOffset == -1 {
		// newBootNum not in original boot order
		newData = make([]byte, len(origData)+2)
		binary.LittleEndian.PutUint16(newData, newBootNum)
		copy(newData[2:], origData)
	} else {
		newData = make([]byte, len(origData))
		binary.LittleEndian.PutUint16(newData, newBootNum)
		copy(newData[2:], origData[:optionOffset])
		copy(newData[optionOffset+2:], origData[optionOffset+2:])
	}
	return efiWriteVariable("BootOrder", efi.GlobalVariable, attrs, newData)
}

// SetEfiBootVariables sets the Boot#### and BootOrder variables for the given
// load option information.
//
// Constructs an EFI load option with the given description, the device path
// corresponding to the given asset path, and the given optional data. Writes
// the EFI boot variable Boot#### to contain the resulting load option. Then,
// sets the BootOrder variable so that the #### number from the chosen Boot####
// is first, removing it from elsewhere in the BootOrder if it occurs.
func SetEfiBootVariables(description string, assetPath string, optionalData []byte) error {
	loadOptionData, err := constructLoadOption(description, assetPath, optionalData)
	if err != nil {
		return err
	}
	bootNum, err := setEfiBootOptionVariable(loadOptionData)
	if err != nil {
		return err
	}
	return setEfiBootOrderVariable(bootNum)
}

// setUbuntuSeedEfiBootVariables sets EFI variables according to the bootloader
// found on ubuntu seed if it is a UefiBootloader.
func setUbuntuSeedEfiBootVariables() error {
	opts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	// Set EFI boot variables according to bootloader on ubuntu-seed
	seedBl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return fmt.Errorf("cannot find bootloader in seed directory: %v; skipping set1ting EFI variables", err)
	}
	ubl, ok := seedBl.(bootloader.UefiBootloader)
	if !ok {
		return errUnsupportedBootloader
	}
	description, assetPath, optionalData, err := ubl.ParametersForEfiLoadOption()
	if err != nil {
		return fmt.Errorf("cannot get EFI load option parameter: %v", err)
	}
	if err = SetEfiBootVariables(description, assetPath, optionalData); err != nil {
		return fmt.Errorf("failed to set EFI boot variables: %v", err)
	}
	return nil
}

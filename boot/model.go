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

package boot

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// DeviceChange handles a change of the underlying device. Specifically it can
// be used during remodel when a new device is associated with a new model. The
// encryption keys will be resealed for both models. The device model file which
// is measured during boot will be updated. The recovery systems that belong to
// the old model will no longer be usable.
func DeviceChange(from Device, to Device) error {
	if !to.HasModeenv() {
		// nothing useful happens on a non-UC20 system here
		return nil
	}

	m, err := loadModeenv()
	if err != nil {
		return err
	}

	newModel := to.Model()
	oldModel := from.Model()
	// modeenv fields and the file are changed atomically, it's enough to
	// check just this field
	modified := false
	if m.TryModel != newModel.Model() {
		// we either haven't been here yet, or a reboot occurred after
		// try model was cleared and modeenv was rewritten
		m.useThisTryModel(newModel)
		modified = true
	}
	if m.Model != oldModel.Model() {
		// a modeenv with new model was already written, restore
		// the 'expected' original state, the model file on disk
		// will match one of the models
		m.useThisModel(oldModel)
		modified = true
	}
	if modified {
		if err := m.Write(); err != nil {
			return err
		}
	}

	// reseal with both models now, such that we'd still be able to boot
	// even if there is a reboot before the device/model file is updated, or
	// before the final reseal with one model
	const expectReseal = true
	if err := resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal); err != nil {
		// best effort clear the modeenv's try model
		m.clearTryModel()
		if mErr := m.Write(); mErr != nil {
			return fmt.Errorf("%v (restoring modeenv failed: %v)", err, mErr)
		}
		return err
	}

	// update the device model file in boot (we may be overwriting the same
	// model file if we reached this place before a reboot has occurred)
	if err := writeModelToUbuntuBoot(to.Model()); err != nil {
		err = fmt.Errorf("cannot write new model file: %v", err)
		// the file has not been modified, so just clear the try model
		m.clearTryModel()
		if mErr := m.Write(); mErr != nil {
			return fmt.Errorf("%v (restoring modeenv failed: %v)", err, mErr)
		}
		return err
	}

	// now we can update the model to the new one
	m.useThisModel(newModel)
	// and clear the try model
	m.clearTryModel()

	if err := m.Write(); err != nil {
		// modeenv has not been written and still contains both the old
		// and a new model, but the model file has been modified,
		// restore the original model file
		if restoreErr := writeModelToUbuntuBoot(from.Model()); restoreErr != nil {
			return fmt.Errorf("%v (restoring model failed: %v)", err, restoreErr)
		}
		// however writing modeenv failed, so trying to clear the model
		// and write it again could be pointless, let the failure
		// percolate up the stack
		return err
	}

	// past a successful reseal, the old recovery systems become unusable and will
	// not be able to access the data anymore
	if err := resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal); err != nil {
		// resealing failed, but modeenv and the file have been modified

		// first restore the modeenv in case we reboot, such that if the
		// post reboot code reseals, it will allow both models (in case
		// even more reboots occur)
		m.useThisModel(from.Model())
		m.useThisTryModel(newModel)
		if mErr := m.Write(); mErr != nil {
			return fmt.Errorf("%v (writing modeenv failed: %v)", err, mErr)
		}

		// restore the original model file (we have resealed for both
		// models previously)
		if restoreErr := writeModelToUbuntuBoot(from.Model()); restoreErr != nil {
			return fmt.Errorf("%v (restoring model failed: %v)", err, restoreErr)
		}

		// drop the tried model
		m.clearTryModel()
		if mErr := m.Write(); mErr != nil {
			return fmt.Errorf("%v (restoring modeenv failed: %v)", err, mErr)
		}

		// resealing failed, so no point in trying it again
		return err
	}
	return nil
}

func writeModelToUbuntuBoot(model *asserts.Model) error {
	modelPath := filepath.Join(InitramfsUbuntuBootDir, "device/model")
	f, err := osutil.NewAtomicFile(modelPath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return err
	}
	defer f.Cancel()
	if err := asserts.NewEncoder(f).Encode(model); err != nil {
		return err
	}
	return f.Commit()
}

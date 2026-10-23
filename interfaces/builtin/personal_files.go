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

package builtin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
)

const personalFilesSummary = `allows access to personal files or directories`

const personalFilesBaseDeclarationPlugs = `
  personal-files:
    allow-installation: false
    deny-auto-connection: true
`

const personalFilesBaseDeclarationSlots = `
  personal-files:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const personalFilesConnectedPlugAppArmor = `
# Description: Can access specific personal files or directories in the 
# users's home directory.
# This is restricted because it gives file access to arbitrary locations.
`

type personalFilesInterface struct {
	commonFilesInterface
}

func validateSinglePathHome(np string) error {
	if !strings.HasPrefix(np, "$HOME/") {
		return fmt.Errorf(`%q must start with "$HOME/"`, np)
	}
	if strings.Count(np, "$HOME") > 1 {
		return fmt.Errorf(`$HOME must only be used at the start of the path of %q`, np)
	}
	return nil
}

func init() {
	registerIface(&personalFilesInterface{
		commonFilesInterface{
			commonInterface: commonInterface{
				name:                 "personal-files",
				summary:              personalFilesSummary,
				implicitOnCore:       true,
				implicitOnClassic:    true,
				baseDeclarationPlugs: personalFilesBaseDeclarationPlugs,
				baseDeclarationSlots: personalFilesBaseDeclarationSlots,
			},
			apparmorHeader:    personalFilesConnectedPlugAppArmor,
			extraPathValidate: validateSinglePathHome,
		},
	})
}

// potentiallyMissingDirs returns an ensure directory specification that contains the information
// required to create potentially missing directories for the given path. Potentially missing
// directories are those that are implicitly required in order to enable the user to create
// the target directory specified in a write attribute path.
func potentiallyMissingDirs(path string) (*interfaces.EnsureDirSpec, error) {
	// All directories between $HOME and the leaf directory are potentially missing
	path = filepath.Clean(path)
	pathElements := strings.Split(path, string(filepath.Separator))
	if pathElements[0] != "$HOME" {
		// BeforePreparePlug should prevent this
		return nil, fmt.Errorf(`%q must start with "$HOME/"`, path)
	}
	if len(pathElements) < 3 {
		return nil, nil
	}

	return &interfaces.EnsureDirSpec{
		// EnsureDir prefix directory that must exist
		MustExistDir: "$HOME",
		// Directory to ensure by creating the missing directories within MustExistDir
		EnsureDir: filepath.Join(pathElements[:len(pathElements)-1]...),
	}, nil
}

var dirsToEnsure = func(paths []string) ([]*interfaces.EnsureDirSpec, error) {
	var ensureDirSpecs []*interfaces.EnsureDirSpec
	for _, path := range paths {
		ensureDirSpec, err := potentiallyMissingDirs(path)
		if err != nil {
			return nil, err
		}
		if ensureDirSpec != nil {
			ensureDirSpecs = append(ensureDirSpecs, ensureDirSpec)
		}
	}
	return ensureDirSpecs, nil
}

func (iface *personalFilesInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Create missing directories for write paths only.
	// BeforePreparePlug should prevent error.
	writes, _ := stringListAttribute(plug, "write")
	ensureDirSpecs, err := dirsToEnsure(writes)
	if err != nil {
		return fmt.Errorf("cannot connect plug %s: %v", plug.Name(), err)
	}
	if len(ensureDirSpecs) > 0 {
		if err = spec.AddUserEnsureDirs(ensureDirSpecs); err != nil {
			return fmt.Errorf("cannot connect plug %s: %v", plug.Name(), err)
		}
	}
	return nil
}

func (iface *personalFilesInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if err := iface.commonFilesInterface.AppArmorConnectedPlug(spec, plug, slot); err != nil {
		return err
	}

	// Create missing directories for write paths only.
	// BeforePreparePlug should prevent error.
	writes, _ := stringListAttribute(plug, "write")

	// Add snippet for snap-update-ns
	ensureDirSpecs, err := dirsToEnsure(writes)
	if err != nil {
		return fmt.Errorf("cannot connect plug %s: %v", plug.Name(), err)
	}
	if len(ensureDirSpecs) > 0 {
		spec.AddEnsureDirMounts(iface.commonFilesInterface.commonInterface.name, ensureDirSpecs)
	}

	return nil
}

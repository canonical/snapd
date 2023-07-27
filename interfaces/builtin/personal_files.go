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

func dirsToCreate(rawPaths []interface{}) ([]*interfaces.EnsureDirSpec, error) {
	var ensureDirSpecs []*interfaces.EnsureDirSpec
	for _, rawPath := range rawPaths {
		ensureDirSpec, err := dirDependency(rawPath)
		if err != nil {
			return nil, err
		}
		if ensureDirSpec != nil {
			ensureDirSpecs = append(ensureDirSpecs, ensureDirSpec)
		}
	}

	return ensureDirSpecs, nil
}

// dirDependency calculates the directory specification containing the information
// required to create the missing parent directories of the provided raw path.
// Only supports paths that start with $HOME.
func dirDependency(rawPath interface{}) (*interfaces.EnsureDirSpec, error) {
	path, ok := rawPath.(string)
	if !ok {
		return nil, fmt.Errorf("%[1]v (%[1]T) is not a string", rawPath)
	}

	// The minimum viable path consists of three path elements, starting with
	// directory $HOME and looks like $HOME/<dependant-dir-1>/<target-dir-or-file>
	pathElements := strings.Split(path, string(filepath.Separator))
	if len(pathElements) < 3 {
		return nil, nil
	}
	if pathElements[0] != "$HOME" {
		return nil, nil
	}

	ensureDirSpec := &interfaces.EnsureDirSpec{
		// EnsureDir prefix directory that must exist
		MustExistDir: pathElements[0],
		// Directory to ensure by creating the missing directories within MustExistDir
		EnsureDir: filepath.Join(pathElements[:len(pathElements)-1]...),
	}

	return ensureDirSpec, nil
}

func (iface *personalFilesInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Create missing directories for write paths only
	var writes []interface{}
	_ = plug.Attr("write", &writes)

	ensureDirSpecs, err := dirsToCreate(writes)
	if err != nil {
		return fmt.Errorf("cannot connect plug %s: %v", plug.Name(), err)
	}
	if len(ensureDirSpecs) > 0 {
		spec.AddEnsureDirs(ensureDirSpecs)
	}

	return nil
}

func (iface *personalFilesInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if err := iface.commonFilesInterface.AppArmorConnectedPlug(spec, plug, slot); err != nil {
		return err
	}

	var writes []interface{}
	_ = plug.Attr("write", &writes)

	// Add snippet for snap-update-ns
	ensureDirSpecs, err := dirsToCreate(writes)
	if err != nil {
		return fmt.Errorf("cannot connect plug %s: %v", plug.Name(), err)
	}
	if len(ensureDirSpecs) > 0 {
		spec.AllowEnsureDirMounts(iface.commonFilesInterface.commonInterface.name, ensureDirSpecs)
	}

	return nil
}

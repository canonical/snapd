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

package builtin

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/snap"
)

const kernelModuleLoadSummary = `allows constrained control over kernel module loading`

const kernelModuleLoadBaseDeclarationPlugs = `
  kernel-module-load:
    allow-installation: false
    deny-auto-connection: true
`

const kernelModuleLoadBaseDeclarationSlots = `
  kernel-module-load:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
`

var modulesAttrTypeError = errors.New(`kernel-module-load "modules" attribute must be a list of dictionaries`)

// kernelModuleLoadInterface allows creating transient and persistent modules
type kernelModuleLoadInterface struct {
	commonInterface
}

type loadOption int

const (
	loadNone loadOption = iota
	loadDenied
	loadOnBoot
)

type ModuleInfo struct {
	name    string
	load    loadOption
	options string
}

var kernelModuleNameRegexp = regexp.MustCompile(`^[-a-zA-Z0-9_]+$`)
var kernelModuleOptionsRegexp = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_]*(=[[:graph:]]+)? *)+$`)

func enumerateModules(plug interfaces.Attrer, handleModule func(moduleInfo *ModuleInfo) error) error {
	modulesAttr, ok := plug.Lookup("modules")
	if !ok {
		return nil
	}
	modules, ok := modulesAttr.([]interface{})
	if !ok {
		return modulesAttrTypeError
	}

	for _, m := range modules {
		module, ok := m.(map[string]interface{})
		if !ok {
			return modulesAttrTypeError
		}

		name, ok := module["name"].(string)
		if !ok {
			return errors.New(`kernel-module-load "name" must be a string`)
		}

		var load loadOption
		if loadAttr, found := module["load"]; found {
			loadString, ok := loadAttr.(string)
			if !ok {
				return errors.New(`kernel-module-load "load" must be a string`)
			}

			switch loadString {
			case "denied":
				load = loadDenied
			case "on-boot":
				load = loadOnBoot
			default:
				return fmt.Errorf(`kernel-module-load "load" value is unrecognized: %q`, loadString)
			}
		}

		var options string
		if optionsAttr, found := module["options"]; found {
			options, ok = optionsAttr.(string)
			if !ok {
				return errors.New(`kernel-module-load "options" must be a string`)
			}
		}

		moduleInfo := &ModuleInfo{
			name:    name,
			load:    load,
			options: options,
		}

		if err := handleModule(moduleInfo); err != nil {
			return err
		}
	}

	return nil
}

func validateNameAttr(name string) error {
	if !kernelModuleNameRegexp.MatchString(name) {
		return errors.New(`kernel-module-load "name" attribute is not a valid module name`)
	}

	return nil
}

func validateOptionsAttr(moduleInfo *ModuleInfo) error {
	if moduleInfo.options == "" {
		return nil
	}

	if moduleInfo.load == loadDenied {
		return errors.New(`kernel-module-load "options" attribute incompatible with "load: denied"`)
	}

	if !kernelModuleOptionsRegexp.MatchString(moduleInfo.options) {
		return fmt.Errorf(`kernel-module-load "options" attribute contains invalid characters: %q`, moduleInfo.options)
	}

	return nil
}

func validateModuleInfo(moduleInfo *ModuleInfo) error {
	if err := validateNameAttr(moduleInfo.name); err != nil {
		return err
	}

	if err := validateOptionsAttr(moduleInfo); err != nil {
		return err
	}

	if moduleInfo.options == "" && moduleInfo.load == loadNone {
		return errors.New(`kernel-module-load: must specify at least "load" or "options"`)
	}

	return nil
}

func (iface *kernelModuleLoadInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	numModulesEntries := 0
	err := enumerateModules(plug, func(moduleInfo *ModuleInfo) error {
		numModulesEntries++
		return validateModuleInfo(moduleInfo)
	})
	if err != nil {
		return err
	}

	if numModulesEntries == 0 {
		return modulesAttrTypeError
	}

	return nil
}

func (iface *kernelModuleLoadInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	err := enumerateModules(plug, func(moduleInfo *ModuleInfo) error {
		var err error
		switch moduleInfo.load {
		case loadDenied:
			err = spec.DisallowModule(moduleInfo.name)
		case loadOnBoot:
			err = spec.AddModule(moduleInfo.name)
			if err != nil {
				break
			}
			fallthrough
		case loadNone:
			if len(moduleInfo.options) > 0 {
				err = spec.SetModuleOptions(moduleInfo.name, moduleInfo.options)
			}
		default:
			// we can panic, this will be catched on validation
			panic("Unsupported module load option")
		}
		return err
	})
	return err
}

func (iface *kernelModuleLoadInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&kernelModuleLoadInterface{
		commonInterface: commonInterface{
			name:                 "kernel-module-load",
			summary:              kernelModuleLoadSummary,
			baseDeclarationPlugs: kernelModuleLoadBaseDeclarationPlugs,
			baseDeclarationSlots: kernelModuleLoadBaseDeclarationSlots,
			implicitOnCore:       true,
			implicitOnClassic:    true,
		},
	})
}

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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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
	loadDynamic
)

type ModuleInfo struct {
	name    string
	load    loadOption
	options string
}

var (
	kernelModuleNameRegexp    = regexp.MustCompile(`^[-a-zA-Z0-9_]+$`)
	kernelModuleOptionsRegexp = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_]*(=[[:graph:]]+)? *)+$`)
)

func enumerateModules(plug interfaces.Attrer, handleModule func(moduleInfo *ModuleInfo) error) error {
	var modules []map[string]interface{}
	mylog.Check(plug.Attr("modules", &modules))
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		return modulesAttrTypeError
	}

	for _, module := range modules {
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
			case "dynamic":
				load = loadDynamic
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
		mylog.Check(handleModule(moduleInfo))

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

	dynamicLoadingWithAnyOptions := moduleInfo.load == loadDynamic && moduleInfo.options == "*"
	if !dynamicLoadingWithAnyOptions && !kernelModuleOptionsRegexp.MatchString(moduleInfo.options) {
		return fmt.Errorf(`kernel-module-load "options" attribute contains invalid characters: %q`, moduleInfo.options)
	}

	return nil
}

func validateModuleInfo(moduleInfo *ModuleInfo) error {
	mylog.Check(validateNameAttr(moduleInfo.name))
	mylog.Check(validateOptionsAttr(moduleInfo))

	if moduleInfo.options == "" && moduleInfo.load == loadNone {
		return errors.New(`kernel-module-load: must specify at least "load" or "options"`)
	}

	return nil
}

func (iface *kernelModuleLoadInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	numModulesEntries := 0
	mylog.Check(enumerateModules(plug, func(moduleInfo *ModuleInfo) error {
		numModulesEntries++
		return validateModuleInfo(moduleInfo)
	}))

	if numModulesEntries == 0 {
		return modulesAttrTypeError
	}

	return nil
}

func (iface *kernelModuleLoadInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snapInfo := plug.Snap()
	commonDataDir := snapInfo.CommonDataDir()
	mylog.Check(enumerateModules(plug, func(moduleInfo *ModuleInfo) error {
		switch moduleInfo.load {
		case loadDenied:
			mylog.Check(spec.DisallowModule(moduleInfo.name))
		case loadOnBoot:
			mylog.Check(spec.AddModule(moduleInfo.name))

			fallthrough
		case loadNone, loadDynamic:
			if len(moduleInfo.options) > 0 && moduleInfo.options != "*" {
				// module options might include filesystem paths. Beside
				// supporting hardcoded paths, it makes sense to support also
				// paths to files provided by the snap; for this reason, we
				// support expanding the $SNAP_COMMON variable here.
				// We do not use os.Expand() because that supports both $ENV
				// and ${ENV}, and we'd rather not alter the options which
				// contain a "$" but are not meant to be expanded. Instead,
				// just look for the "$SNAP_COMMON/" string and replace it; the
				// extra "/" at the end ensures that the variable is
				// terminated.
				options := strings.ReplaceAll(moduleInfo.options, "$SNAP_COMMON/", commonDataDir+"/")
				mylog.Check(spec.SetModuleOptions(moduleInfo.name, options))
			}
		default:
			// we can panic, this will be catched on validation
			panic("Unsupported module load option")
		}
		return err
	}))
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

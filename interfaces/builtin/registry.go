// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
)

// The registry interface can be plugged by snaps that want to access specific
// registry views. Plugs are auto-connected if the registry's account is the
// same as the snap's publisher.
const registrySummary = `allows accessing a registry through a view`

const registryBaseDeclarationSlots = `
  registry:
    allow-installation:
      slot-snap-type:
        - core
`
const registryBaseDeclarationPlugs = `
  registry:
    allow-auto-connection:
      plug-attributes:
        account: $PLUG_PUBLISHER_ID
`

// registryInterface allows accessing a registry through a view
type registryInterface struct {
	commonInterface
}

func (iface *registryInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	account, ok := plug.Attrs["account"].(string)
	if !ok || account == "" {
		return fmt.Errorf(`registry plug must have an "account" attribute`)
	}
	if !asserts.IsValidAccountID(account) {
		return fmt.Errorf(`registry plug must have a valid "account" attribute: format mismatch`)
	}

	view, ok := plug.Attrs["view"].(string)
	if !ok || view == "" {
		return fmt.Errorf(`registry plug must have a "view" attribute`)
	}

	if err := validateView(view); err != nil {
		return fmt.Errorf(`registry plug must have a valid "view" attribute: %w`, err)
	}

	role, ok := plug.Attrs["role"].(string)
	if ok && role != "manager" {
		return fmt.Errorf(`optional registry plug "role" attribute must be "manager"`)
	}

	return nil
}

func validateView(view string) error {
	parts := strings.Split(view, "/")
	if len(parts) != 2 {
		return fmt.Errorf("expected registry and view names separated by a single '/': %s", view)
	}

	if !registry.ValidRegistryName.MatchString(parts[0]) {
		return fmt.Errorf("invalid registry name: %s does not match '%s'", parts[0], registry.ValidRegistryName.String())
	}

	if !registry.ValidViewName.MatchString(parts[1]) {
		return fmt.Errorf("invalid view name: %s does not match '%s'", parts[1], registry.ValidViewName.String())
	}

	return nil
}

func init() {
	registerIface(&registryInterface{
		commonInterface: commonInterface{
			name:                 "registry",
			summary:              registrySummary,
			baseDeclarationPlugs: registryBaseDeclarationPlugs,
			baseDeclarationSlots: registryBaseDeclarationSlots,
			implicitOnClassic:    true,
			implicitOnCore:       true,
		}})
}

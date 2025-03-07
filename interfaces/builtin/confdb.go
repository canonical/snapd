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
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/snap"
)

// The confdb interface can be plugged by snaps that want to access specific
// confdb views. Plugs are auto-connected if the confdb's account is the
// same as the snap's publisher.
const confdbSummary = `allows accessing a confdb through a view`

const confdbBaseDeclarationSlots = `
  confdb:
    allow-installation:
      slot-snap-type:
        - core
`
const confdbBaseDeclarationPlugs = `
  confdb:
    allow-auto-connection:
      plug-attributes:
        account: $PLUG_PUBLISHER_ID
`

// confdbInterface allows accessing a confdb through a view
type confdbInterface struct {
	commonInterface
}

func (iface *confdbInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	account, ok := plug.Attrs["account"].(string)
	if !ok || account == "" {
		return fmt.Errorf(`confdb plug must have an "account" attribute`)
	}
	if !asserts.IsValidAccountID(account) {
		return fmt.Errorf(`confdb plug must have a valid "account" attribute: format mismatch`)
	}

	view, ok := plug.Attrs["view"].(string)
	if !ok || view == "" {
		return fmt.Errorf(`confdb plug must have a "view" attribute`)
	}

	if err := validateView(view); err != nil {
		return fmt.Errorf(`confdb plug must have a valid "view" attribute: %w`, err)
	}

	// by default, snaps can read/write confdb and be notified of changes. The
	// custodian role allows snaps to change, reject and persist changes made by others
	role, ok := plug.Attrs["role"].(string)
	if ok && role != "custodian" {
		return fmt.Errorf(`optional confdb plug "role" attribute must be "custodian"`)
	}

	return nil
}

func validateView(view string) error {
	parts := strings.Split(view, "/")
	if len(parts) != 2 {
		return fmt.Errorf("expected confdb and view names separated by a single '/': %s", view)
	}

	if !confdb.ValidConfdbName.MatchString(parts[0]) {
		return fmt.Errorf("invalid confdb name: %s does not match '%s'", parts[0], confdb.ValidConfdbName.String())
	}

	if !confdb.ValidViewName.MatchString(parts[1]) {
		return fmt.Errorf("invalid view name: %s does not match '%s'", parts[1], confdb.ValidViewName.String())
	}

	return nil
}

func init() {
	registerIface(&confdbInterface{
		commonInterface: commonInterface{
			name:                 "confdb",
			summary:              confdbSummary,
			baseDeclarationPlugs: confdbBaseDeclarationPlugs,
			baseDeclarationSlots: confdbBaseDeclarationSlots,
			implicitOnClassic:    true,
			implicitOnCore:       true,
		}})
}

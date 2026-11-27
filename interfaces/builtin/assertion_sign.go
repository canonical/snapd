// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
)

const assertionSignSummary = `allows signing assertions mediated by snapd`

const assertionSignBaseDeclarationPlugs = `
  assertion-sign:
    allow-installation: false
    deny-auto-connection: true
`

const assertionSignBaseDeclarationSlots = `
  assertion-sign:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// The interface grants no new AppArmor rules: every restricted snap already
// has /run/snapd-snap.socket (base AppArmor template) and the daemon gates the
// sign endpoint by the connection + peer credentials (ucred security tag ->
// snap instance name), see requireInterfaceApiAccessImpl in daemon/access.go.

type assertionSignInterface struct {
	commonInterface
}

// BeforePreparePlug validates plug attributes at connection time so an
// invalid allow-list fails fast (at connect) rather than at signing time.
//
// The "assertion-types" attribute is mandatory (resolved O4, deny-all):
// signing is a security-critical grant, so the package maintainer must
// declare the specific assertion types the snap may request signatures for.
// That declaration ships alongside the allow-installation request for the
// plug, making the requested signing scope visible to the store review and
// to the user at connect time. A plug without the attribute is useless with
// the handler's fail-closed semantics, so rejecting it at install/connect
// time gives the clearest feedback.
func (i *assertionSignInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	types, ok := plug.Attrs["assertion-types"]
	if !ok {
		// O4 (resolved): omitted "assertion-types" means the connection may
		// not sign any assertion type (deny-all). Fail early and explicitly
		// instead of leaving a plug that can never succeed.
		return fmt.Errorf(`plug attribute "assertion-types" is required: list the assertion types (e.g. ["model"]) this snap may sign; an omitted attribute is a deny-all grant (see the assertion-sign interface docs)`)
	}
	list, ok := types.([]any)
	if !ok || len(list) == 0 {
		return fmt.Errorf(`plug attribute "assertion-types" must be a non-empty list of assertion type names, e.g. ["model"]`)
	}
	for _, t := range list {
		s, ok := t.(string)
		if !ok || asserts.Type(s) == nil {
			return fmt.Errorf("unknown assertion type %v in plug attribute \"assertion-types\"", t)
		}
	}
	return nil
}

func init() {
	registerIface(&assertionSignInterface{commonInterface{
		name:    "assertion-sign",
		summary: assertionSignSummary,
		// The slot is an OS-provided (implicit) slot, like ros-snapd-support
		// and snapd-control: it must materialize on the OS snap on both core
		// and classic systems. The *plug* is not implicit: base-declaration
		// allow-installation: false means only snaps with an explicit
		// allow-installation request can use it.
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: assertionSignBaseDeclarationPlugs,
		baseDeclarationSlots: assertionSignBaseDeclarationSlots,
	}})
}

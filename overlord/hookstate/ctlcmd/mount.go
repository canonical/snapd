// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package ctlcmd

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
)

var (
	shortMountHelp = i18n.G("Create a temporary or permanent mount")
	longMountHelp  = i18n.G(`
The mount command mounts the given source onto the given destination path,
provided that the snap has a plug for the mount-control interface which allows
this operation.`)
)

func init() {
	addCommand("mount", shortMountHelp, longMountHelp, func() command { return &mountCommand{} })
}

type mountCommand struct {
	baseCommand
	Positional struct {
		What  string `positional-arg-name:"<what>" required:"yes" description:"path to the resource to be mounted"`
		Where string `positional-arg-name:"<where>" required:"yes" description:"path to the destination mount point"`
	} `positional-args:"yes" required:"yes"`
	Persistent  bool   `long:"persistent" description:"make the mount persist across reboots"`
	Type        string `long:"type" short:"t" description:"filesystem type"`
	Options     string `long:"options" short:"o" description:"comma-separated list of mount options"`
	snapInfo    *snap.Info
	optionsList []string
}

func matchMountPathAttribute(path string, attribute interface{}, snapInfo *snap.Info) bool {
	pattern, ok := attribute.(string)
	if !ok {
		return false
	}

	expandedPattern := snapInfo.ExpandSnapVariables(pattern)

	const allowCommas = true
	pp, err := utils.NewPathPattern(expandedPattern, allowCommas)
	return err == nil && pp.Matches(path)
}

// matchConnection checks whether the given mount connection attributes give
// the snap permission to execute the mount command
func (m *mountCommand) matchConnection(attributes map[string]interface{}) bool {
	if !matchMountPathAttribute(m.Positional.What, attributes["what"], m.snapInfo) {
		return false
	}

	if !matchMountPathAttribute(m.Positional.Where, attributes["where"], m.snapInfo) {
		return false
	}

	if m.Type != "" {
		if types, ok := attributes["type"].([]interface{}); ok {
			found := false
			for _, iface := range types {
				if typeString, ok := iface.(string); ok && typeString == m.Type {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		} else {
			return false
		}
	} else {
		// The filesystem type was not given; we let it through only if the
		// plug also did not specify a type.
		if _, typeIsSet := attributes["type"]; typeIsSet {
			return false
		}
	}

	if optionsIfaces, ok := attributes["options"].([]interface{}); ok {
		var allowedOptions []string
		for _, iface := range optionsIfaces {
			if option, ok := iface.(string); ok {
				allowedOptions = append(allowedOptions, option)
			}
		}
		for _, option := range m.optionsList {
			if !strutil.ListContains(allowedOptions, option) {
				return false
			}
		}
	}

	if m.Persistent {
		if allowedPersistent, ok := attributes["persistent"].(bool); !ok || !allowedPersistent {
			return false
		}
	}

	return true
}

// checkConnections checks whether the established connections give the snap
// permission to execute the mount command
func (m *mountCommand) checkConnections(context *hookstate.Context) error {
	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	m.snapInfo, err = snapstate.CurrentInfo(st, snapName)
	if err != nil {
		return fmt.Errorf("internal error: cannot get snap info: %s", err)
	}

	for connId, connState := range conns {
		if connState.Interface != "mount-control" {
			continue
		}

		if !connState.Active() {
			continue
		}

		connRef, err := interfaces.ParseConnRef(connId)
		if err != nil {
			return err
		}

		if connRef.PlugRef.Snap != snapName {
			continue
		}

		mounts, ok := connState.StaticPlugAttrs["mount"].([]interface{})
		if !ok {
			continue
		}

		for _, mountAttributes := range mounts {
			if m.matchConnection(mountAttributes.(map[string]interface{})) {
				return nil
			}
		}
	}
	return fmt.Errorf("no matching mount-control connection found")
}

func (m *mountCommand) ensureMount(sysd systemd.Systemd) (string, error) {
	snapName := m.snapInfo.InstanceName()
	revision := m.snapInfo.SnapRevision().String()
	lifetime := systemd.Transient
	if m.Persistent {
		lifetime = systemd.Persistent
	}
	unitName, err := sysd.EnsureMountUnitFileWithOptions(&systemd.MountUnitOptions{
		Lifetime:    lifetime,
		Description: fmt.Sprintf("Mount unit for %s, revision %s via mount-control", snapName, revision),
		What:        m.Positional.What,
		Where:       m.Positional.Where,
		Fstype:      m.Type,
		Options:     m.optionsList,
		Origin:      "mount-control",
	})
	if err != nil {
		_ = sysd.RemoveMountUnitFile(m.Positional.Where)
	}
	return unitName, err
}

func (m *mountCommand) Execute([]string) error {
	context, err := m.ensureContext()
	if err != nil {
		return err
	}

	// Parse the mount options into an array
	for _, option := range strings.Split(m.Options, ",") {
		if option != "" {
			m.optionsList = append(m.optionsList, option)
		}
	}

	if err := m.checkConnections(context); err != nil {
		snapName := context.InstanceName()
		return fmt.Errorf("snap %q lacks permissions to create the requested mount: %v", snapName, err)
	}

	sysd := systemd.New(systemd.SystemMode, nil)
	_, err = m.ensureMount(sysd)
	if err != nil {
		return fmt.Errorf("cannot ensure mount unit: %v", err)
	}

	return nil
}

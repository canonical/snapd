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
	"github.com/snapcore/snapd/snap"
)

const dotfilesSummary = `allows access to specific hidden files in the home directory`

const dotfilesBaseDeclarationSlots = `
  dotfiles:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dotfilesConnectedPlugAppArmor = `
# Description: Can access specific hidden files in user's $HOME. This is restricted
# because it gives file access to some of the user's $HOME.
`

type dotfilesInterface struct {
	commonInterface
}

func validatePaths(attrName string, paths []interface{}) error {
	for _, npp := range paths {
		np, ok := npp.(string)
		if !ok {
			return fmt.Errorf("%q must be a list of strings", attrName)
		}
		// filepath.Clean() will remove trailing "/" but we allow this
		// for "paths"
		if attrName == "dirs" {
			last := len(np)
			if np[last-1] == '/' {
				np = np[:last-1]
			}
		}

		// FIXME: should we ensure that no other "$" is part of the string?
		if !strings.HasPrefix(np, "/") && !strings.HasPrefix(np, "$HOME/") {
			return fmt.Errorf(`%q must start with "/" or "$HOME"`, np)
		}
		p := filepath.Clean(np)
		if p != np {
			return fmt.Errorf("%q must be clean", np)
		}

		if strings.Contains(p, "..") {
			return fmt.Errorf(`%q contains invalid ".."`, p)
		}
		if strings.Contains(p, "~") {
			return fmt.Errorf(`%q contains invalid "~"`, p)
		}
		if err := apparmor.ValidateFreeFromAARE(p); err != nil {
			return err
		}
	}
	return nil
}

func (iface *dotfilesInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	hasValidAttr := false
	for _, att := range []string{"files", "dirs"} {
		if _, ok := plug.Attrs[att]; !ok {
			continue
		}
		il, ok := plug.Attrs[att].([]interface{})
		if !ok {
			return fmt.Errorf("cannot add dotfiles plug: %q must be a list of strings", att)
		}
		if err := validatePaths(att, il); err != nil {
			return fmt.Errorf("cannot add dotfiles plug: %s", err)
		}
		hasValidAttr = true
	}
	if !hasValidAttr {
		return fmt.Errorf(`cannot add dotfiles plug: needs valid "files" or "dirs" attribute`)
	}

	return nil
}

func formatPath(ip interface{}) (string, error) {
	p, ok := ip.(string)
	if !ok {
		return "", fmt.Errorf("%[1]v (%[1]T) is not a string", ip)
	}
	prefix := ""
	if strings.Count(p, "$HOME") > 0 {
		p = strings.Replace(p, "$HOME", "@${HOME}", 1)
		prefix = "owner "
	}
	return filepath.Clean(prefix + p), nil
}

func (iface *dotfilesInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var files, dirs []interface{}
	_ = plug.Attr("files", &files)
	_ = plug.Attr("dirs", &dirs)

	errPrefix := fmt.Sprintf(`cannot connect plug %s: `, plug.Name())
	spec.AddSnippet(dotfilesConnectedPlugAppArmor)
	for _, file := range files {
		f, err := formatPath(file)
		if err != nil {
			return fmt.Errorf(errPrefix+"%v", err)
		}
		s := fmt.Sprintf("%s rwkl,", f)
		spec.AddSnippet(s)
	}
	for _, dir := range dirs {
		d, err := formatPath(dir)
		if err != nil {
			return fmt.Errorf(errPrefix+"%v", err)
		}
		s := fmt.Sprintf("%s/** rwkl,", d)
		spec.AddSnippet(s)
	}

	return nil
}

func init() {
	registerIface(&dotfilesInterface{commonInterface{
		name:                 "dotfiles",
		summary:              dotfilesSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: dotfilesBaseDeclarationSlots,
		reservedForOS:        true,
	}})
}

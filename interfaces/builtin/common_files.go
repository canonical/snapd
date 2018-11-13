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
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

type commonFilesInterface struct {
	commonInterface

	apparmorHeader    string
	extraPathValidate func(string) error
}

func formatPath(ip interface{}) (string, error) {
	p, ok := ip.(string)
	if !ok {
		return "", fmt.Errorf("%[1]v (%[1]T) is not a string", ip)
	}
	prefix := ""
	if strings.Count(p, "$HOME") > 0 {
		p = strings.Replace(p, "$HOME", "@{HOME}", 1)
		prefix = "owner "
	}
	p += "{,/,/**}"

	return fmt.Sprintf("%s%q", prefix, filepath.Clean(p)), nil
}

func allowPathAccess(buf *bytes.Buffer, perm string, paths []interface{}) error {
	for _, rawPath := range paths {
		p, err := formatPath(rawPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(buf, "%s %s\n", p, perm)
	}
	return nil
}

func (iface *commonFilesInterface) validatePaths(attrName string, paths []interface{}) error {
	for _, npp := range paths {
		np, ok := npp.(string)
		if !ok {
			return fmt.Errorf("%q must be a list of strings", attrName)
		}
		if err := iface.validateSinglePath(np); err != nil {
			return err
		}
	}
	return nil
}

func (iface *commonFilesInterface) validateSinglePath(np string) error {
	if strings.HasSuffix(np, "/") {
		return fmt.Errorf(`%q cannot end with "/"`, np)
	}
	if strings.Contains(np, "@{") {
		return fmt.Errorf(`%q should not use "@{"`, np)
	}
	p := filepath.Clean(np)
	if p != np {
		return fmt.Errorf("%q must be clean", np)
	}
	if strings.Contains(p, "~") {
		return fmt.Errorf(`%q contains invalid "~"`, p)
	}
	if err := apparmor.ValidateNoAppArmorRegexp(p); err != nil {
		return err
	}

	if err := iface.extraPathValidate(np); err != nil {
		return err
	}
	return nil
}

func (iface *commonFilesInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	hasValidAttr := false
	for _, att := range []string{"read", "write"} {
		if _, ok := plug.Attrs[att]; !ok {
			continue
		}
		il, ok := plug.Attrs[att].([]interface{})
		if !ok {
			return fmt.Errorf("cannot add %s plug: %q must be a list of strings", iface.name, att)
		}
		if err := iface.validatePaths(att, il); err != nil {
			return fmt.Errorf("cannot add %s plug: %s", iface.name, err)
		}
		hasValidAttr = true
	}
	if !hasValidAttr {
		return fmt.Errorf(`cannot add %s plug: needs valid "read" or "write" attribute`, iface.name)
	}

	return nil
}

func (iface *commonFilesInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var reads, writes []interface{}
	_ = plug.Attr("read", &reads)
	_ = plug.Attr("write", &writes)

	errPrefix := fmt.Sprintf(`cannot connect plug %s: `, plug.Name())
	buf := bytes.NewBufferString(iface.apparmorHeader)
	if err := allowPathAccess(buf, "rk,", reads); err != nil {
		return fmt.Errorf("%s%v", errPrefix, err)
	}
	if err := allowPathAccess(buf, "rwkl,", writes); err != nil {
		return fmt.Errorf("%s%v", errPrefix, err)
	}
	spec.AddSnippet(buf.String())

	return nil
}

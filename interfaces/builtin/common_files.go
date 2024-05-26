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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

type commonFilesInterface struct {
	commonInterface

	apparmorHeader    string
	extraPathValidate func(string) error
}

// filesAAPerm can either be files{Read,Write} and converted to a string
// expands into the right apparmor permissions for the files interface.
type filesAAPerm int

const (
	filesRead filesAAPerm = iota
	filesWrite
)

func (a filesAAPerm) String() string {
	switch a {
	case filesRead:
		return "rk" // [r]ead and loc[k]
	case filesWrite:
		return "rwkl" // [r]ead, [w]rite, loc[k] and [l]ink//
	}
	panic(fmt.Sprintf("invalid perm: %d", a))
}

func formatPath(ip interface{}) (string, error) {
	p, ok := ip.(string)
	if !ok {
		return "", fmt.Errorf("%[1]v (%[1]T) is not a string", ip)
	}
	prefix := ""
	// Note that the {personal,system}-files interface impose
	// limitations on the $HOME usage - system-files forbids it,
	// personal only allows starting with $HOME in the path.
	if strings.Contains(p, "$HOME") {
		p = strings.Replace(p, "$HOME", "@{HOME}", -1)
		prefix = "owner "
	}
	p = filepath.Clean(p)
	p += "{,/,/**}"

	return fmt.Sprintf("%s%q", prefix, p), nil
}

func allowPathAccess(buf *bytes.Buffer, perm filesAAPerm, paths []interface{}) error {
	for _, rawPath := range paths {
		p := mylog.Check2(formatPath(rawPath))

		fmt.Fprintf(buf, "%s %s,\n", p, perm)
	}
	return nil
}

func (iface *commonFilesInterface) validatePaths(attrName string, paths []interface{}) error {
	for _, npp := range paths {
		np, ok := npp.(string)
		if !ok {
			return fmt.Errorf("%q must be a list of strings", attrName)
		}
		mylog.Check(iface.validateSinglePath(np))

	}
	return nil
}

func (iface *commonFilesInterface) validateSinglePath(np string) error {
	if strings.HasSuffix(np, "/") {
		return fmt.Errorf(`%q cannot end with "/"`, np)
	}
	p := filepath.Clean(np)
	if p != np {
		return fmt.Errorf("cannot use %q: try %q", np, filepath.Clean(np))
	}
	if strings.Contains(p, "~") {
		return fmt.Errorf(`%q cannot contain "~"`, p)
	}
	mylog.Check(apparmor_sandbox.ValidateNoAppArmorRegexp(p))

	// extraPathValidation must be implemented by the interface
	// that build on top of the abstract commonFilesInterface
	if iface.extraPathValidate == nil {
		panic("extraPathValidate must be set when using the commonFilesInterface")
	}
	mylog.Check(iface.extraPathValidate(np))

	return nil
}

func (iface *commonFilesInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	hasValidAttr := false
	for _, att := range []string{"read", "write"} {
		if _, ok := plug.Attrs[att]; !ok {
			continue
		}
		paths, ok := plug.Attrs[att].([]interface{})
		if !ok {
			return fmt.Errorf("cannot add %s plug: %q must be a list of strings", iface.name, att)
		}
		mylog.Check(iface.validatePaths(att, paths))

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
	mylog.Check(allowPathAccess(buf, filesRead, reads))
	mylog.Check(allowPathAccess(buf, filesWrite, writes))

	spec.AddSnippet(buf.String())

	return nil
}

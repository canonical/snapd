// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package apparmor

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/snap"
)

// legacyVariablees returns text defining some apparmor variables that work
// with legacy apparmor templates.
//
// The variables are expanded by apparmor parser. They are (currently):
//  - APP_APPNAME
//  - APP_ID_DBUS
//  - APP_PKGNAME_DBUS
//  - APP_PKGNAME
//  - APP_VERSION
//  - INSTALL_DIR
// They can be changed but this has to match changes in template.go.
//
// In addition, the set of variables listed here interacts with old-security
// interface since there the base template is provided by a particular 3rd
// party snap, not by snappy.
func legacyVariables(appInfo *snap.AppInfo) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{APP_APPNAME}=\"%s\"\n", appInfo.Name)
	// TODO: replace with app.SecurityTag()
	fmt.Fprintf(&buf, "@{APP_ID_DBUS}=\"%s\"\n",
		dbus.SafePath(fmt.Sprintf("%s.%s_%s_%s",
			appInfo.Snap.Name, appInfo.Snap.Developer, appInfo.Name, appInfo.Snap.Version)))
	// XXX: How is this different from APP_ID_DBUS?
	fmt.Fprintf(&buf, "@{APP_PKGNAME_DBUS}=\"%s\"\n",
		dbus.SafePath(fmt.Sprintf("%s.%s", appInfo.Snap.Name, appInfo.Snap.Developer)))
	// TODO: stop using .Developer, investigate how this is used.
	fmt.Fprintf(&buf, "@{APP_PKGNAME}=\"%s.%s\"\n", appInfo.Snap.Name, appInfo.Snap.Developer)
	// TODO: switch to .Revision
	fmt.Fprintf(&buf, "@{APP_VERSION}=\"%s\"\n", appInfo.Snap.Version)
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"{/snaps,/gadget}\"")
	return buf.Bytes()
}

// modenVariables returns text defining some apparmor variables that
// work with non-legacy apparmor templates.
//
// XXX: Straw-man: can we just expose the following apparmor variables...
//
// @{APP_NAME}=app.Name
// @{APP_SECURITY_TAG}=app.SecurityTag()
// @{SNAP_NAME}=app.SnapName
//
// ...have everything work correctly?
func modernVariables(appInfo *snap.AppInfo) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{APP_NAME}=\"%s\"\n", appInfo.Name)
	fmt.Fprintf(&buf, "@{APP_SECURITY_TAG}=\"%s\"\n", interfaces.SecurityTag(appInfo))
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", appInfo.Snap.Name)
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"{/snaps,/gadget}\"")
	return buf.Bytes()
}

var (
	templatePattern = regexp.MustCompile("(###[^#]+###)")
	// XXX: This needs to be verified by security team.
	attachPattern  = regexp.MustCompile(`\(attach_disconnected\)`)
	attachComplain = []byte("(attach_disconnected,complain)")
)

// aaHeader returns the topmost part of the generated apparmor profile.
//
// The header contains a few lines of apparmor variables that are referenced by
// the template as well as the syntax that begins the content of the actual
// profile. That same content also decides if the profile is enforcing or
// advisory (complain). This is used to implement developer mode.
func (b *Backend) aaHeader(appInfo *snap.AppInfo, developerMode bool) []byte {
	header := b.legacyTemplate
	if header == nil {
		header = []byte(defaultTemplate)
	}
	header = bytes.TrimRight(header, "\n}")
	if developerMode {
		header = attachPattern.ReplaceAll(header, attachComplain)
	}
	return templatePattern.ReplaceAllFunc(header, func(in []byte) []byte {
		switch string(in) {
		case "###VAR###":
			// TODO: use modern variables when default template is compatible
			// with them and the custom template is not used.
			return legacyVariables(appInfo)
		case "###PROFILEATTACH###":
			return []byte(fmt.Sprintf("profile \"%s\"", interfaces.SecurityTag(appInfo)))
		}
		return nil
	})
}

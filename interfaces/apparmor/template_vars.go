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

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/snap"
)

// writeLegacyVariablees writes definitions of apparmor variables that work
// with legacy apparmor templates to the specified buffer.
//
// The variables are expanded by apparmor parser. They are (currently):
//  - APP_APPNAME
//  - APP_ID_DBUS
//  - APP_PKGNAME_DBUS
//  - APP_PKGNAME
//  - APP_VERSION
//  - INSTALL_DIR
//
// In addition, the set of variables listed here interacts with old-security
// interface. The old-security interface allows snap developers to bundle a
// custom tepmlate and those are expected to be compatible with variables
// defined in 15.04.
func writeLegacyVariables(buf *bytes.Buffer, appInfo *snap.AppInfo) {
	fmt.Fprintf(buf, "@{APP_APPNAME}=\"%s\"\n", appInfo.Name)
	// TODO: replace with app.SecurityTag()
	fmt.Fprintf(buf, "@{APP_ID_DBUS}=\"%s\"\n",
		dbus.SafePath(fmt.Sprintf("%s.%s_%s_%s",
			appInfo.Snap.Name, appInfo.Snap.Developer, appInfo.Name, appInfo.Snap.Version)))
	// XXX: How is this different from APP_ID_DBUS?
	fmt.Fprintf(buf, "@{APP_PKGNAME_DBUS}=\"%s\"\n",
		dbus.SafePath(fmt.Sprintf("%s.%s", appInfo.Snap.Name, appInfo.Snap.Developer)))
	// TODO: stop using .Developer, investigate how this is used.
	fmt.Fprintf(buf, "@{APP_PKGNAME}=\"%s.%s\"\n", appInfo.Snap.Name, appInfo.Snap.Developer)
	// TODO: switch to .Revision
	fmt.Fprintf(buf, "@{APP_VERSION}=\"%s\"\n", appInfo.Snap.Version)
	fmt.Fprintf(buf, "@{INSTALL_DIR}=\"{/snaps,/gadget}\"\n")
}

// writeModenVariables writes definition of apparmor variables that work with
// non-legacy apparmor templates to the specified buffer.
//
// XXX: Straw-man: can we just expose the following apparmor variables...
//
// @{APP_NAME}=app.Name
// @{APP_SECURITY_TAG}=app.SecurityTag()
// @{SNAP_NAME}=app.SnapName
//
// ...have everything work correctly?
func writeModernVariables(buf *bytes.Buffer, appInfo *snap.AppInfo) {
	fmt.Fprintf(buf, "@{APP_NAME}=\"%s\"\n", appInfo.Name)
	fmt.Fprintf(buf, "@{APP_SECURITY_TAG}=\"%s\"\n", interfaces.SecurityTag(appInfo))
	fmt.Fprintf(buf, "@{SNAP_NAME}=\"%s\"\n", appInfo.Snap.Name)
	fmt.Fprintf(buf, "@{INSTALL_DIR}=\"{/snaps,/gadget}\"\n")
}

// writeProfileAttach writes the profile attachment line of the apparmor profile.
//
// The line typically looks like this:
//
//   profile "snap.http.GET" (attach_disconnected) {
func writeProfileAttach(buf *bytes.Buffer, appInfo *snap.AppInfo, developerMode bool) {
	fmt.Fprintf(buf, "profile \"%s\" (attach_disconnected", interfaces.SecurityTag(appInfo))
	if developerMode {
		// XXX: This needs to be verified by security team.
		fmt.Fprintf(buf, ",complain")
	}
	fmt.Fprintf(buf, ") {\n")
}

const (
	placeholderVar           = "###VAR###"
	placeholderProfileAttach = "###PROFILEATTACH###"
	expectedProfileHeader    = "###PROFILEATTACH### (attach_disconnected) {"
)

// parseTemplate processes an apparmor template
func parseTemplate(template []byte) (imports, body []byte, err error) {
	varStart := bytes.Index(template, []byte(placeholderVar))
	if varStart == -1 {
		return nil, nil, fmt.Errorf("cannot find placeholder: %q", placeholderVar)
	}
	varEndOffset := bytes.IndexRune(template[varStart:], '\n')
	if varEndOffset == -1 {
		return nil, nil, fmt.Errorf("missing newline after placeholder: %q", placeholderVar)
	}
	profileAttachStart := bytes.Index(template, []byte(placeholderProfileAttach))
	if profileAttachStart == -1 {
		return nil, nil, fmt.Errorf("cannot find placeholder: %q", placeholderProfileAttach)
	}
	profileAttachEndOffset := bytes.IndexRune(template[profileAttachStart:], '\n')
	if profileAttachEndOffset == -1 {
		return nil, nil, fmt.Errorf("missing newline after placeholder: %q", placeholderProfileAttach)
	}
	if string(template[profileAttachStart:profileAttachStart+profileAttachEndOffset]) != expectedProfileHeader {
		return nil, nil, fmt.Errorf("unsupported profile header (wanted: %s)", expectedProfileHeader)
	}
	curlyBraceIndex := bytes.LastIndexByte(template, '}')
	if curlyBraceIndex == -1 {
		return nil, nil, fmt.Errorf("missing closing curly brace")
	}
	imports = template[:varStart]
	// NOTE: +1 gets rid of the newline so that we get just the body
	body = template[profileAttachStart+profileAttachEndOffset+1 : curlyBraceIndex]
	return imports, body, nil
}

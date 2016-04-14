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

	"github.com/ubuntu-core/snappy/snap"
)

// legacyVariablees returns text defining some apparmor variables that work
// with legacy apparmor templates.
//
// The variables are expanded by apparmor parser. They are (currently):
//  - APP_APPNAME
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
	fmt.Fprintf(&buf, "@{APP_PKGNAME}=\"%s\"\n", appInfo.Snap.Name())
	fmt.Fprintf(&buf, "@{APP_VERSION}=\"%d\"\n", appInfo.Snap.Revision)
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"/snap\"")
	return buf.Bytes()
}

// modernVariables returns text defining some apparmor variables that
// work with non-legacy apparmor templates.
func modernVariables(appInfo *snap.AppInfo) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{APP_NAME}=\"%s\"\n", appInfo.Name)
	fmt.Fprintf(&buf, "@{APP_SECURITY_TAG}=\"%s\"\n", appInfo.SecurityTag())
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", appInfo.Snap.Name())
	fmt.Fprintf(&buf, "@{SNAP_REVISION}=\"%d\"\n", appInfo.Snap.Revision)
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"/snap\"")
	return buf.Bytes()
}

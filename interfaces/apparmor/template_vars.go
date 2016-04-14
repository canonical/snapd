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

// modernVariables returns text defining some apparmor variables that
// work with non-legacy apparmor templates.
func modernVariables(appInfo *snap.AppInfo) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "@{APP_NAME}=\"%s\"\n", appInfo.Name)
	fmt.Fprintf(&buf, "@{SNAP_NAME}=\"%s\"\n", appInfo.Snap.Name())
	fmt.Fprintf(&buf, "@{SNAP_REVISION}=\"%d\"\n", appInfo.Snap.Revision)
	fmt.Fprintf(&buf, "@{INSTALL_DIR}=\"/snap\"")
	return buf.Bytes()
}

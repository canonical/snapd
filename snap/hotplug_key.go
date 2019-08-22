// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C)2019 Canonical Ltd
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

package snap

import (
	"fmt"
)

// HotplugKey is a string key of a hotplugged device
type HotplugKey string

// ShortString returns a truncated string representation of the hotplug key
func (h HotplugKey) ShortString() string {
	str := string(h)
	// hotplug key is sha256 (64+1 characters long), output just the first 12 characters.
	return fmt.Sprintf("%.12s…", str)
}

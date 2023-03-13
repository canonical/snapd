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

// Package apparmor contains primitives for working with apparmor.
//
// References:
//   - http://wiki.apparmor.net/index.php/Kernel_interfaces
//   - http://apparmor.wiki.kernel.org/
//   - http://manpages.ubuntu.com/manpages/xenial/en/man7/apparmor.7.html
package apparmor

import (
	"fmt"
	"strings"
)

// ValidateNoAppArmorRegexp will check that the given string does not
// contain AppArmor regular expressions (AARE), double quotes or \0.
// Note that to check the inverse of this, that is that a string has
// valid AARE, one should use interfaces/utils.NewPathPattern().
func ValidateNoAppArmorRegexp(s string) error {
	const AARE = `?*[]{}^"` + "\x00"

	if strings.ContainsAny(s, AARE) {
		return fmt.Errorf("%q contains a reserved apparmor char from %s", s, AARE)
	}
	return nil
}

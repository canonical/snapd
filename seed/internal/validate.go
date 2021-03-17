// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package internal

import (
	"fmt"
	"regexp"
)

// validSeedSystemLabel is the regex describing a valid system label. Typically
// system labels are expected to be date based, eg. 20201116, but for
// completeness follow the same rule as model names (incl. one letter model
// names and thus system labels), with the exception that uppercase letters are
// not allowed, as the systems will often be stored in a FAT filesystem.
var validSeedSystemLabel = regexp.MustCompile("^[a-z0-9](?:-?[a-z0-9])*$")

// ValidateSeedSystemLabel checks whether the string is a valid UC20 seed system
// label.
func ValidateUC20SeedSystemLabel(label string) error {
	if !validSeedSystemLabel.MatchString(label) {
		return fmt.Errorf("invalid seed system label: %q", label)
	}
	return nil
}

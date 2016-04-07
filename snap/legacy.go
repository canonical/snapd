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

package snap

import (
	"github.com/ubuntu-core/snappy/snap/legacygadget"
)

// LegacyYaml collects the legacy fields in snap.yaml that are up to be reworked.
type LegacyYaml struct {
	// gadget snap only
	Gadget legacygadget.Gadget       `yaml:"gadget,omitempty"`
	Config legacygadget.SystemConfig `yaml:"config,omitempty"`

	// legacy kernel snap support
	Kernel string `yaml:"kernel,omitempty"`
	Initrd string `yaml:"initrd,omitempty"`
	Dtbs   string `yaml:"dtbs,omitempty"`
}

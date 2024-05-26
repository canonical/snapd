// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package naming

import (
	"fmt"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// ComponentRef contains the component name and the owner snap name.
type ComponentRef struct {
	SnapName      string `yaml:"snap-name" json:"snap-name"`
	ComponentName string `yaml:"component-name" json:"component-name"`
}

// NewComponentRef returns a reference to a snap component.
func NewComponentRef(snapName, componentName string) ComponentRef {
	return ComponentRef{SnapName: snapName, ComponentName: componentName}
}

func (cr ComponentRef) String() string {
	return fmt.Sprintf("%s+%s", cr.SnapName, cr.ComponentName)
}

// Validate validates the component.
func (cr ComponentRef) Validate() error {
	for _, name := range []string{cr.SnapName, cr.ComponentName} {
		mylog.Check(
			// Same restrictions as snap names
			ValidateSnap(name))
	}
	return nil
}

func (cid *ComponentRef) UnmarshalYAML(unmarshall func(interface{}) error) error {
	idStr := ""
	mylog.Check(unmarshall(&idStr))

	names := strings.Split(idStr, "+")
	if len(names) != 2 {
		return fmt.Errorf("incorrect component name %q", idStr)
	}

	*cid = ComponentRef{SnapName: names[0], ComponentName: names[1]}

	return nil
}

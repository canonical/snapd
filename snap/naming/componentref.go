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
	"regexp"
	"strings"
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

// SplitFullComponentName splits <snap>+<comp> in <snap> and <comp> strings.
func SplitFullComponentName(fullComp string) (string, string, error) {
	names := strings.Split(fullComp, "+")
	if len(names) != 2 {
		return "", "", fmt.Errorf("incorrect component name %q", fullComp)
	}

	return names[0], names[1], nil
}

func (cr ComponentRef) String() string {
	return fmt.Sprintf("%s+%s", cr.SnapName, cr.ComponentName)
}

// Validate validates the component.
func (cr ComponentRef) Validate() error {
	for _, name := range []string{cr.SnapName, cr.ComponentName} {
		// Same restrictions as snap names
		if err := ValidateSnap(name); err != nil {
			return err
		}
	}
	return nil
}

func (cid *ComponentRef) UnmarshalYAML(unmarshall func(any) error) error {
	idStr := ""
	if err := unmarshall(&idStr); err != nil {
		return err
	}

	snap, comp, err := SplitFullComponentName(idStr)
	if err != nil {
		return err
	}

	*cid = ComponentRef{SnapName: snap, ComponentName: comp}

	return nil
}

// snapPackComponentFilename is a regular expression that matches what snap pack
// creates when building a component. For example, "foo+bar_1.0.0.comp" matches
// this expression.
var snapPackComponentFilename = regexp.MustCompile(fmt.Sprintf(`^(%[1]s)\+(%[1]s)(?:_.*)?\.comp$`, almostValidNameRegexString))

// ComponentRefFromSnapPackFilename parses a filename created when creating a
// component with "snap pack". These are generally in one of two forms:
//   - <snap>+<comp>.comp
//   - <snap>+<comp>_<version>.comp
func ComponentRefFromSnapPackFilename(filename string) (ComponentRef, error) {
	matches := snapPackComponentFilename.FindStringSubmatch(filename)
	if len(matches) != 3 {
		return ComponentRef{}, fmt.Errorf("cannot parse snap pack component filename: %q", filename)
	}
	return NewComponentRef(matches[1], matches[2]), nil
}

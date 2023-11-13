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

package snap

import (
	"fmt"

	"github.com/snapcore/snapd/snap/naming"
	"gopkg.in/yaml.v2"
)

// ComponentInfo is the content of a component.yaml file.
type ComponentInfo struct {
	Component   naming.ComponentRef `yaml:"component"`
	Type        ComponentType       `yaml:"type"`
	Version     string              `yaml:"version"`
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description"`
}

// ComponentSideInfo is the equivalent of SideInfo for components, and
// includes relevant information for which the canonical source is a
// snap store.
type ComponentSideInfo struct {
	Component naming.ComponentRef `json:"component"`
	Revision  Revision            `json:"revision"`
}

// ReadComponentInfoFromContainer reads ComponentInfo from a snap component container.
func ReadComponentInfoFromContainer(compf Container) (*ComponentInfo, error) {
	yamlData, err := compf.ReadFile("meta/component.yaml")
	if err != nil {
		return nil, err
	}

	var ci ComponentInfo

	if err := yaml.UnmarshalStrict(yamlData, &ci); err != nil {
		return nil, fmt.Errorf("cannot parse component.yaml: %s", err)
	}

	if err := ci.validate(); err != nil {
		return nil, err
	}

	return &ci, nil
}

// FullName returns the full name of the component, which is composed
// by snap name and component name.
func (ci *ComponentInfo) FullName() string {
	return ci.Component.String()
}

// Validate performs some basic validations on component.yaml values.
func (ci *ComponentInfo) validate() error {
	if ci.Component.SnapName == "" {
		return fmt.Errorf("snap name for component cannot be empty")
	}
	if ci.Component.ComponentName == "" {
		return fmt.Errorf("component name cannot be empty")
	}
	if err := ci.Component.Validate(); err != nil {
		return err
	}
	if ci.Type == "" {
		return fmt.Errorf("component type cannot be empty")
	}
	// version is optional
	if ci.Version != "" {
		if err := ValidateVersion(ci.Version); err != nil {
			return err
		}
	}
	if err := ValidateSummary(ci.Summary); err != nil {
		return err
	}
	if err := ValidateDescription(ci.Description); err != nil {
		return err
	}
	return nil
}

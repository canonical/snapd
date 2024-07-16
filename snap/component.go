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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap/naming"
	"gopkg.in/yaml.v2"
)

// ComponentInfo contains information about a snap component.
type ComponentInfo struct {
	Component           naming.ComponentRef `yaml:"component"`
	Type                ComponentType       `yaml:"type"`
	Version             string              `yaml:"version"`
	Summary             string              `yaml:"summary"`
	Description         string              `yaml:"description"`
	ComponentProvenance string              `yaml:"provenance,omitempty"`

	// Hooks contains information about implicit and explicit hooks that this
	// component has. This information is derived from a combination on the
	// component itself and the snap.Info that represents the snap this
	// component is associated with. This field may be empty if the
	// ComponentInfo was not created with the help of a snap.Info.
	Hooks map[string]*HookInfo `yaml:"-"`

	// ComponentSideInfo contains information for which the source of truth is
	// not the component blob itself.
	ComponentSideInfo
}

// Provenance returns the provenance of the component. This returns
// naming.DefaultProvenance if no value is set explicitly in the component
// metadata.
func (ci *ComponentInfo) Provenance() string {
	if ci.ComponentProvenance == "" {
		return naming.DefaultProvenance
	}
	return ci.ComponentProvenance
}

// NewComponentInfo creates a new ComponentInfo.
func NewComponentInfo(cref naming.ComponentRef, ctype ComponentType, version, summary, description, provenance string, csi *ComponentSideInfo) *ComponentInfo {
	if csi == nil {
		csi = &ComponentSideInfo{}
	}

	return &ComponentInfo{
		Component:           cref,
		Type:                ctype,
		Version:             version,
		Summary:             summary,
		Description:         description,
		ComponentProvenance: provenance,
		ComponentSideInfo:   *csi,
	}
}

// ComponentSideInfo is the equivalent of SideInfo for components, and
// includes relevant information for which the canonical source is a
// snap store.
type ComponentSideInfo struct {
	Component naming.ComponentRef `json:"component"`
	Revision  Revision            `json:"revision"`
}

// NewComponentSideInfo creates a new ComponentSideInfo.
func NewComponentSideInfo(cref naming.ComponentRef, rev Revision) *ComponentSideInfo {
	return &ComponentSideInfo{
		Component: cref,
		Revision:  rev,
	}
}

// Equal compares two ComponentSideInfo.
func (csi *ComponentSideInfo) Equal(other *ComponentSideInfo) bool {
	return *csi == *other
}

// ComponentBaseDir returns where components are to be found for the
// snap with name instanceName.
func ComponentsBaseDir(instanceName string) string {
	return filepath.Join(BaseDir(instanceName), "components")
}

// componentPlaceInfo holds information about where to put a component in the
// system. It implements ContainerPlaceInfo and should be used only via this
// interface.
type componentPlaceInfo struct {
	// Name and revision for the component
	compName     string
	compRevision Revision
	// snapInstance identifies the snap that uses this component.
	snapInstance string
}

var _ ContainerPlaceInfo = (*componentPlaceInfo)(nil)

// MinimalComponentContainerPlaceInfo returns a ContainerPlaceInfo with just
// the location information for a component of the given name and revision that
// is used by a snapInstance.
func MinimalComponentContainerPlaceInfo(compName string, compRev Revision, snapInstance string) ContainerPlaceInfo {
	return &componentPlaceInfo{
		compName:     compName,
		compRevision: compRev,
		snapInstance: snapInstance,
	}
}

// ContainerName returns the component name.
func (c *componentPlaceInfo) ContainerName() string {
	return fmt.Sprintf("%s+%s", c.snapInstance, c.compName)
}

// Filename returns the container file name.
func (c *componentPlaceInfo) Filename() string {
	return filepath.Base(c.MountFile())
}

// MountDir returns the directory where a component gets mounted, which
// will be of the form:
// /snaps/<snap_instance>/components/mnt/<component_name>/<component_revision>
func (c *componentPlaceInfo) MountDir() string {
	return ComponentMountDir(c.compName, c.compRevision, c.snapInstance)
}

// MountFile returns the path of the file to be mounted for a component,
// which will be of the form /var/lib/snaps/snaps/<snap>+<comp>_<rev>.comp
func (c *componentPlaceInfo) MountFile() string {
	return filepath.Join(dirs.SnapBlobDir,
		fmt.Sprintf("%s_%s.comp", c.ContainerName(), c.compRevision))
}

// MountDescription returns the mount unit Description field.
func (c *componentPlaceInfo) MountDescription() string {
	return fmt.Sprintf("Mount unit for %s, revision %s", c.ContainerName(), c.compRevision)
}

// ComponentLinkPath returns the path for the symlink for a component for a
// given snap revision. Note that this function only uses the ContainerName
// method on the ContainerPlaceInfo. If that changes, callers of this function
// may need to change how the parameters are initialized.
func ComponentLinkPath(cpi ContainerPlaceInfo, snapRev Revision) string {
	instanceName, compName, _ := strings.Cut(cpi.ContainerName(), "+")
	compBase := ComponentsBaseDir(instanceName)
	return filepath.Join(compBase, snapRev.String(), compName)
}

// ComponentInstallDate returns the "install date" of the component by checking
// when its symlink was created. We cannot use the mount directory as lstat
// returns the date of the root of the container instead of the date when the
// mount directory was created.
func ComponentInstallDate(cpi ContainerPlaceInfo, snapRev Revision) *time.Time {
	symLn := ComponentLinkPath(cpi, snapRev)
	if st, err := os.Lstat(symLn); err == nil {
		modTime := st.ModTime()
		return &modTime
	}
	return nil
}

// ComponentSize returns the file size of a component.
func ComponentSize(cpi ContainerPlaceInfo) (int64, error) {
	st, err := os.Lstat(cpi.MountFile())
	if err != nil {
		return 0, fmt.Errorf("error while looking for component file %q: %v",
			cpi.MountFile(), err)
	}
	if !st.Mode().IsRegular() {
		return 0, fmt.Errorf("unexpected file type for component file %q", cpi.MountFile())
	}
	return st.Size(), nil
}

// ReadComponentInfoFromContainer reads ComponentInfo from a snap component
// container. If snapInfo is not nil, it is used to complete the ComponentInfo
// information about the component's implicit and explicit hooks, and their
// associated plugs. If snapInfo is not nil, consistency checks are performed to
// ensure that the component is a component of the provided snap. Additionally,
// an optional ComponentSideInfo can be passed to fill in the ComponentInfo's
// ComponentSideInfo field.
func ReadComponentInfoFromContainer(compf Container, snapInfo *Info, csi *ComponentSideInfo) (*ComponentInfo, error) {
	yamlData, err := compf.ReadFile("meta/component.yaml")
	if err != nil {
		return nil, err
	}

	componentInfo, err := InfoFromComponentYaml(yamlData)
	if err != nil {
		return nil, err
	}

	if csi != nil {
		componentInfo.ComponentSideInfo = *csi
	}

	// if snapInfo is nil, then we can't complete the component info with
	// implicit and explicit hooks, so we return the component info as is.
	//
	// we could technically create the hooks, but would be unable to bind plugs
	// to them, so it is probably best to just leave them out.
	if snapInfo == nil {
		return componentInfo, nil
	}

	if snapInfo.SnapName() != componentInfo.Component.SnapName {
		return nil, fmt.Errorf(
			"component %q is not a component for snap %q", componentInfo.Component, snapInfo.SnapName())
	}

	componentName := componentInfo.Component.ComponentName

	component, ok := snapInfo.Components[componentName]
	if !ok {
		return nil, fmt.Errorf("%q is not a component for snap %q", componentName, snapInfo.SnapName())
	}

	if component.Type != componentInfo.Type {
		return nil, fmt.Errorf("inconsistent component type (%q in snap, %q in component)", component.Type, componentInfo.Type)
	}

	// attach the explicit hooks, these are defined in the snap.yaml. plugs are
	// already bound to the hooks.
	componentInfo.Hooks = component.ExplicitHooks

	// attach the implicit hooks, these are not defined in the snap.yaml.
	// unscoped plugs are bound to the implicit hooks here.
	addAndBindImplicitComponentHooksFromContainer(compf, componentInfo, component, snapInfo)

	return componentInfo, nil
}

func addAndBindImplicitComponentHooksFromContainer(compf Container, componentInfo *ComponentInfo, component *Component, info *Info) {
	hooks, err := compf.ListDir("meta/hooks")
	if err != nil {
		return
	}

	for _, hook := range hooks {
		addAndBindImplicitComponentHook(componentInfo, info, component, hook)
	}
}

func addAndBindImplicitComponentHook(componentInfo *ComponentInfo, snapInfo *Info, component *Component, hook string) {
	// don't overwrite a hook that has already been loaded from the snap.yaml
	if _, ok := componentInfo.Hooks[hook]; ok {
		return
	}

	if !IsComponentHookSupported(hook) {
		logger.Noticef("ignoring unsupported implicit hook %q for component %q", componentInfo.Component, hook)
		return
	}

	// implicit hooks get all unscoped plugs
	unscopedPlugs := make(map[string]*PlugInfo)
	for name, plug := range snapInfo.Plugs {
		if plug.Unscoped {
			unscopedPlugs[name] = plug
		}
	}

	// TODO: if hooks ever get slots, then unscoped slots will need to be
	// bound here

	componentInfo.Hooks[hook] = &HookInfo{
		Snap:      snapInfo,
		Component: component,
		Name:      hook,
		Plugs:     unscopedPlugs,
		Explicit:  false,
	}
}

// InfoFromComponentYaml parses a ComponentInfo from the raw yaml data.
func InfoFromComponentYaml(compYaml []byte) (*ComponentInfo, error) {
	var ci ComponentInfo

	if err := yaml.UnmarshalStrict(compYaml, &ci); err != nil {
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

// HooksForPlug returns the component hooks that are associated with the given
// plug.
func (ci *ComponentInfo) HooksForPlug(plug *PlugInfo) []*HookInfo {
	return hooksForPlug(plug, ci.Hooks)
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
	if err := validateProvenance(ci.ComponentProvenance); err != nil {
		return err
	}
	return nil
}

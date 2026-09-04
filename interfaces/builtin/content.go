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

package builtin

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

const contentSummary = `allows sharing code and data with other snaps`

const contentBaseDeclarationSlots = `
  content:
    allow-installation:
      slot-snap-type:
        - app
        - gadget
        - kernel
    allow-connection:
      plug-attributes:
        -
          content: $SLOT(content)
        -
          compatibility: $SLOT_COMPAT(compatibility)
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
      plug-attributes:
        -
          content: $SLOT(content)
        -
          compatibility: $SLOT_COMPAT(compatibility)
`

// contentInterface allows sharing content between snaps
type contentInterface struct{}

func (iface *contentInterface) Name() string {
	return "content"
}

func (iface *contentInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              contentSummary,
		BaseDeclarationSlots: contentBaseDeclarationSlots,

		AffectsPlugOnRefresh: true,
	}
}

func cleanSubPath(path string) bool {
	return filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
}

func validatePath(path string) error {
	if err := apparmor_sandbox.ValidateNoAppArmorRegexp(path); err != nil {
		return fmt.Errorf("content interface path is invalid: %v", err)
	}
	if ok := cleanSubPath(path); !ok {
		return fmt.Errorf("content interface path is not clean: %q", path)
	}
	return nil
}

// componentPrefix is the marker introducing a component-relative path in a
// content slot attribute, e.g. $SNAP_COMPONENT(mycomp)/share. The same
// constant is also defined in interfaces/builtin/helpers.go (for
// validateSourceDirs) and interfaces/snap_app_set.go (for
// ExpandSliceSnapVariablesWithOrder); keep them in sync.
const componentPrefix = "$SNAP_COMPONENT("

// parseComponentPath decomposes a $SNAP_COMPONENT(<name>)[/<sub>] path.
//
// If the path is not a component path (does not start with componentPrefix),
// isComponent is false and the other return values are empty.
//
// If it is a component path, compName holds the component name and subPath
// holds the remainder (possibly empty, meaning the whole component is shared).
// err is set when the path is malformed, or when the subpath (when present)
// does not pass the same validation as ordinary paths. Whole-component
// sharing is allowed: both $SNAP_COMPONENT(foo) and $SNAP_COMPONENT(foo)/
// resolve with subPath == "".
func parseComponentPath(path string) (compName, subPath string, isComponent bool, err error) {
	if !strings.HasPrefix(path, componentPrefix) {
		return "", "", false, nil
	}
	rest := path[len(componentPrefix):]
	compName, tail, had := strings.Cut(rest, ")")
	if !had || compName == "" || (tail != "" && !strings.HasPrefix(tail, "/")) {
		return "", "", true, fmt.Errorf("invalid format in path %q", path)
	}
	if tail == "" || tail == "/" {
		// Whole-component sharing: $SNAP_COMPONENT(foo) or $SNAP_COMPONENT(foo)/
		return compName, "", true, nil
	}

	// $SNAP_COMPONENT(foo)/bar -> bar
	subPath = tail[1:]
	if err := validatePath(subPath); err != nil {
		return "", "", true, err
	}

	return compName, subPath, true, nil
}

func checkLabelAttributes(attrs map[string]any, nameDef string) error {
	// The ContentCompatLabel feature is checked only at matching time,
	// here we allow the compatibility labels to exist in any case as it
	// has no further side effect.
	content, okContent := attrs["content"].(string)

	// TODO: consider asserting that "content" is a string. right now, a
	// non-string "content" attribute will result in us using the plug's name as
	// the "content" attribute.

	compat, okCompat := attrs["compatibility"].(string)
	if _, ok := attrs["compatibility"]; ok && !okCompat {
		return errors.New("compatibility label must be a string")
	}

	hasContent := okContent && len(content) > 0
	hasCompat := okCompat && len(compat) > 0
	if hasCompat && hasContent {
		return errors.New("cannot have both content and compatibility labels")
	}
	if hasCompat {
		return compatibility.IsValidExpression(compat, nil)
	}
	if hasContent {
		return nil
	}
	// content defaults to nameDef if unspecified and no compatibility label either
	attrs["content"] = nameDef
	return nil
}

func (iface *contentInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	if slot.Attrs == nil {
		slot.Attrs = make(map[string]any)
	}
	if err := checkLabelAttributes(slot.Attrs, slot.Name); err != nil {
		return err
	}

	// Error if "read" or "write" are present alongside "source".
	if _, found := slot.Lookup("source"); found {
		if _, found := slot.Lookup("read"); found {
			return fmt.Errorf(`move the "read" attribute into the "source" section`)
		}
		if _, found := slot.Lookup("write"); found {
			return fmt.Errorf(`move the "write" attribute into the "source" section`)
		}
	}

	// check that we have either a read or write path
	rpath := iface.path(slot, "read")
	wpath := iface.path(slot, "write")
	if len(rpath) == 0 && len(wpath) == 0 {
		return fmt.Errorf("read or write path must be set")
	}

	for _, p := range wpath {
		if _, _, isComp, _ := parseComponentPath(p); isComp {
			// $SNAP_COMPONENT(...) are not allowed for write.
			return fmt.Errorf("component paths can only be used with read, not write: %q", p)
		}

		if err := validatePath(p); err != nil {
			return err
		}
	}
	for _, p := range rpath {
		if compName, _, isComp, err := parseComponentPath(p); isComp {
			// Looks like a $SNAP_COMPONENT(...)
			if err != nil {
				// Which is invalid
				return err
			}
			if _, ok := slot.Snap.Components[compName]; !ok {
				return fmt.Errorf("component %s specified in path %q is not defined in the snap", compName, p)
			}
			continue
		}

		if err := validatePath(p); err != nil {
			return err
		}
	}
	return nil
}

func (iface *contentInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if plug.Attrs == nil {
		plug.Attrs = make(map[string]any)
	}
	if err := checkLabelAttributes(plug.Attrs, plug.Name); err != nil {
		return err
	}

	target, ok := plug.Attrs["target"].(string)
	if !ok || len(target) == 0 {
		return fmt.Errorf("content plug must contain target path")
	}
	if err := validatePath(target); err != nil {
		return err
	}

	return nil
}

// path is an internal helper that extract the "read" and "write" attribute
// of the slot
func (iface *contentInterface) path(attrs interfaces.Attrer, name string) []string {
	if name != "read" && name != "write" {
		panic("internal error, path can only be used with read/write")
	}

	var paths []any
	var source map[string]any

	if err := attrs.Attr("source", &source); err == nil {
		// Access either "source.read" or "source.write" attribute.
		var ok bool
		if paths, ok = source[name].([]any); !ok {
			return nil
		}
	} else {
		// Access either "read" or "write" attribute directly (legacy).
		if err := attrs.Attr(name, &paths); err != nil {
			return nil
		}
	}

	out := make([]string, len(paths))
	for i, p := range paths {
		var ok bool
		out[i], ok = p.(string)
		if !ok {
			return nil
		}
	}
	return out
}

// resolveSpecialVariable resolves one of the three $SNAP* variables at the
// beginning of a given path. The variables are $SNAP, $SNAP_DATA and
// $SNAP_COMMON. If there are no variables then $SNAP is implicitly assumed
// (this is the behavior that was used before the variables were supported). The
// perspective parameter controls how $SNAP is expanded accounting for features
// like parallel installs: PerspectiveOther uses the most precise instance name
// (e.g. snap_key), while PerspectiveSelf uses the snap name (e.g. snap).
func resolveSpecialVariable(path string, snapInfo *snap.Info, perspective snap.ExpandSnapPerspective) string {
	// Content cannot be mounted at arbitrary locations, validate the path
	// for extra safety.
	if err := snap.ValidatePathVariables(path); err == nil && strings.HasPrefix(path, "$") {
		// The path starts with $ and ValidatePathVariables() ensures
		// path contains only $SNAP, $SNAP_DATA, $SNAP_COMMON, and no
		// other $VARs are present.
		return snapInfo.ExpandSnapVariablesSetSnapMountDir(path, dirs.CoreSnapMountDir, perspective)
	}
	// Always prefix with $SNAP if nothing else is provided or the path
	// contains invalid variables.
	return snapInfo.ExpandSnapVariablesSetSnapMountDir(filepath.Join("$SNAP", path), dirs.CoreSnapMountDir, perspective)
}

// resolveComponentSource resolves the source path and the basename to use for
// target derivation for an installed component.
func resolveComponentSource(snapInfo *snap.Info, compInfo *snap.ComponentInfo, subPath string) (
	source, sourceName string,
) {
	compName := compInfo.Component.ComponentName
	// Content-interface paths must be rooted at dirs.CoreSnapMountDir (/snap)
	// so they are accessible inside the snap mount namespace, where /snap is
	// always the bind-mount point regardless of the host's SnapMountDir.
	// TODO this could use a helper in 'snap'
	source = filepath.Clean(filepath.Join(
		dirs.CoreSnapMountDir,
		snapInfo.InstanceName(),
		"components", "mnt",
		compName, compInfo.Revision.String(),
		subPath,
	))
	if subPath == "" {
		// Whole-component sharing: use the component name as the
		// basename so that, with a "source" section present, the target
		// resolves to <target>/<compName> rather than the revision
		// directory.
		sourceName = compName
	}
	return source, sourceName
}

// exportUnderPlugTarget returns true when the content exposed by the slot
// should be placed under the target location named by the plug. This is
// indicated by presence of the 'source' attribute on the slot side.
func exportUnderPlugTarget(slot *interfaces.ConnectedSlot) bool {
	var unused map[string]any
	return slot.Attr("source", &unused) == nil
}

// sourceTarget resolves the source and target paths for a given read/write
// slot path and indicates whether the source of the mount is available.
//
// Specifically in the case of $SNAP_COMPONENT(...) entries, if the component is
// not installed, available is false.
func sourceTarget(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string) (
	source, target string, available bool,
) {
	// The 'target' attribute has already been verified in BeforePreparePlug.
	_ = plug.Attr("target", &target)

	// sourceNameOverride, when non-empty, overrides the default name used in the case
	// we're exporting the source location beneath the target path prescribed in
	// the plug attributes.
	var sourceNameOverride string

	if compName, subPath, isComp, err := parseComponentPath(relSrc); isComp && err == nil {
		ci := slot.AppSet().Component(compName)
		if ci == nil {
			// Component declared but not installed.
			return "", "", false
		}

		source, sourceNameOverride = resolveComponentSource(slot.Snap(), ci, subPath)
	} else {
		// Regular (non-component) $SNAP/$SNAP_DATA/$SNAP_COMMON path.
		source = resolveSpecialVariable(relSrc, slot.Snap(), snap.PerspectiveOther)
	}
	// Target uses PerspectiveSelf as the consumer sees its own snap name.
	target = resolveSpecialVariable(target, plug.Snap(), snap.PerspectiveSelf)

	// Figure out the target path if the source is supposed to be exported on a
	// path beneath the target prescribed in the plug.
	if exportUnderPlugTarget(slot) {
		// unless there's an override, as it is in the case of components, we
		// use the basename by default, e.g.
		// source:
		//   - $SNAP/foo                 -> $TARGET/foo
		//   - $SNAP                     -> $TARGET/<snap-name>
		//   - $SNAP_COMPONENT(bar)/foo  -> $TARGET/foo
		//   - $SNAP_COMPONENT(bar)      -> $TARGET/bar
		sourceName := filepath.Base(source)
		if sourceNameOverride != "" {
			sourceName = sourceNameOverride
		}
		target = filepath.Join(target, sourceName)
	}
	return source, target, true
}

// mountEntry builds the bind-mount entry for a read/write slot path.
//
// available is false if the path references a component that is declared in the
// snap but not installed; the caller should then skip the entry.
func mountEntry(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string, extraOptions ...string) (
	entry osutil.MountEntry, available bool,
) {
	options := make([]string, 0, len(extraOptions)+1)
	options = append(options, "bind")
	options = append(options, extraOptions...)
	source, target, sourceAvailable := sourceTarget(plug, slot, relSrc)
	if !sourceAvailable {
		// unavailable, e.g. could be a component which is not installed
		return osutil.MountEntry{}, false
	}

	return osutil.MountEntry{
		Name:    source,
		Dir:     target,
		Options: options,
	}, true
}

func (iface *contentInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	contentSnippet := bytes.NewBuffer(nil)
	writePaths := iface.path(slot, "write")
	emit := spec.AddUpdateNSf
	if len(writePaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
`)
		for i, w := range writePaths {
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrwklix,\n",
				// Use PerspectiveOther: resolve to provider's precise instance
				// name
				resolveSpecialVariable(w, slot.Snap(), snap.PerspectiveOther))
			// Write paths can never reference components (rejected in
			// BeforePrepareSlot), so ok is always true here.
			source, target, _ := sourceTarget(plug, slot, w)
			emit("  # Read-write content sharing %s -> %s (w#%d)\n", plug.Ref(), slot.Ref(), i)
			emit("  mount options=(bind, rw) \"%s/\" -> \"%s{,-[0-9]*}/\",\n", source, target)
			emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}/\",\n", target)
			emit("  umount \"%s{,-[0-9]*}/\",\n", target)
			// TODO: The assumed prefix depth could be optimized to be more
			// precise since content sharing can only take place in a fixed
			// list of places with well-known paths (well, constrained set of
			// paths). This can be done when the prefix is actually consumed.
			apparmor.GenWritableProfile(emit, source, 1)
			apparmor.GenWritableProfile(emit, target, 1)
			apparmor.GenWritableProfile(emit, fmt.Sprintf("%s-[0-9]*", target), 1)
		}
	}

	readPaths := iface.path(slot, "read")
	if len(readPaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
`)
		for i, r := range readPaths {
			source, target, ok := sourceTarget(plug, slot, r)
			if !ok {
				// Component declared but not installed: skip this path.
				continue
			}
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrkix,\n", source)

			emit("  # Read-only content sharing %s -> %s (r#%d)\n", plug.Ref(), slot.Ref(), i)
			emit("  mount options=(bind) \"%s/\" -> \"%s{,-[0-9]*}/\",\n", source, target)
			emit("  remount options=(bind, ro) \"%s{,-[0-9]*}/\",\n", target)
			emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}/\",\n", target)
			emit("  umount \"%s{,-[0-9]*}/\",\n", target)
			// Look at the TODO comment above.
			apparmor.GenWritableProfile(emit, source, 1)
			apparmor.GenWritableProfile(emit, target, 1)
			apparmor.GenWritableProfile(emit, fmt.Sprintf("%s-[0-9]*", target), 1)
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *contentInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	contentSnippet := bytes.NewBuffer(nil)
	writePaths := iface.path(slot, "write")
	if len(writePaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# When the content interface is writable, allow this slot
# implementation to access the slot's exported files at the plugging
# snap's mountpoint to accommodate software where the plugging app
# tells the slotting app about files to share.
`)
		for _, w := range writePaths {
			_, target, _ := sourceTarget(plug, slot, w)
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrwklix,\n",
				target)
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *contentInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

// Interactions with the mount backend.

func (iface *contentInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, r := range iface.path(slot, "read") {
		me, ok := mountEntry(plug, slot, r, "ro")
		if !ok {
			// Could be a not-installed component entry
			continue
		}
		if err := spec.AddMountEntry(me); err != nil {
			return err
		}
	}
	for _, w := range iface.path(slot, "write") {
		me, ok := mountEntry(plug, slot, w)
		if !ok {
			return fmt.Errorf("internal error: unexpected incomplete write mount entry")
		}
		if err := spec.AddMountEntry(me); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	registerIface(&contentInterface{})
}

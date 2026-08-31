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
	if subPath != "" {
		if err := validatePath(subPath); err != nil {
			return "", "", true, err
		}
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

	// Component paths ($SNAP_COMPONENT(...)) are only permitted in read
	// paths; they are rejected for write paths. Read component paths must
	// reference a component declared in the snap, and their subpath (when
	// present) must pass the same validation as ordinary paths.
	for _, p := range wpath {
		// isComp is checked before err so that even a malformed component
		// path (e.g. dirty subpath) in a write list reports the read-only
		// restriction.
		if _, _, isComp, err := parseComponentPath(p); isComp {
			return fmt.Errorf("component paths can only be used with read, not write: %q", p)
		} else if err != nil {
			return err
		}
		if err := validatePath(p); err != nil {
			return err
		}
	}
	for _, p := range rpath {
		compName, _, isComp, err := parseComponentPath(p)
		if err != nil {
			return err
		}
		if isComp {
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

// componentSource resolves the source path and the basename to use for
// target derivation for an installed component. ok is false if the
// component is declared in the snap but not installed (absent from the
// app set).
func componentSource(slot *interfaces.ConnectedSlot, compName, subPath string) (source, sourceName string, ok bool) {
	ci := slot.AppSet().Component(compName)
	if ci == nil {
		// Component declared but not installed.
		return "", "", false
	}
	// snap.ComponentMountDir is rooted at dirs.SnapMountDir, but content
	// interface paths must use dirs.CoreSnapMountDir to be consistent with
	// $SNAP resolution and visible inside the snap mount namespace (where
	// /snap is the mount point, not /var/lib/snapd/snap).
	compMountDir := snap.ComponentMountDir(compName, ci.Revision, slot.Snap().InstanceName())
	source = filepath.Clean(filepath.Join(
		dirs.CoreSnapMountDir, strings.TrimPrefix(compMountDir, dirs.SnapMountDir), subPath))
	if subPath == "" {
		// Whole-component sharing: use the component name as the
		// basename so that, with a "source" section present, the target
		// resolves to <target>/<compName> rather than the revision
		// directory.
		sourceName = compName
	}
	return source, sourceName, true
}

// sourceTarget resolves the source and target paths for a given read/write
// slot path. For component paths ($SNAP_COMPONENT(...)), the source is the
// component mount directory of the installed component; if the component is
// declared in the snap but not installed (absent from the slot's app set),
// ok is false and the caller should skip the path. For ordinary paths the
// behavior is unchanged and ok is always true.
func sourceTarget(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string) (source, target string, ok bool) {
	// The 'target' attribute has already been verified in BeforePreparePlug.
	_ = plug.Attr("target", &target)

	// sourceName, when non-empty, overrides the basename of source used to
	// derive the target when a "source" section is present. This is used for
	// whole-component sharing where the basename of the component mount
	// directory is the (uninteresting) revision.
	var sourceName string

	if compName, subPath, isComp, err := parseComponentPath(relSrc); err == nil && isComp {
		source, sourceName, ok = componentSource(slot, compName, subPath)
		if !ok {
			return "", "", false
		}
	} else {
		// Errors from parseComponentPath are safely ignored here: the slot
		// has already been vetted in BeforePrepareSlot, which rejects any
		// malformed path before it reaches this code. Non-component paths are
		// resolved as ordinary $SNAP paths.
		source = resolveSpecialVariable(relSrc, slot.Snap(), snap.PerspectiveOther)
	}
	// Target uses PerspectiveSelf as the consumer sees its own snap name.
	target = resolveSpecialVariable(target, plug.Snap(), snap.PerspectiveSelf)

	// Check if the "source" section is present.
	var unused map[string]any
	if err := slot.Attr("source", &unused); err == nil {
		if sourceName == "" {
			_, sourceName = filepath.Split(source)
		}
		target = filepath.Join(target, sourceName)
	}
	return source, target, true
}

// mountEntry builds the bind-mount entry for a read/write slot path. ok is
// false if the path references a component that is declared in the snap but
// not installed; the caller should then skip the entry.
func mountEntry(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string, extraOptions ...string) (osutil.MountEntry, bool) {
	options := make([]string, 0, len(extraOptions)+1)
	options = append(options, "bind")
	options = append(options, extraOptions...)
	source, target, ok := sourceTarget(plug, slot, relSrc)
	return osutil.MountEntry{
		Name:    source,
		Dir:     target,
		Options: options,
	}, ok
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
			// Component declared but not installed: skip this path.
			continue
		}
		if err := spec.AddMountEntry(me); err != nil {
			return err
		}
	}
	for _, w := range iface.path(slot, "write") {
		// Write paths can never reference components (rejected in
		// BeforePrepareSlot), so ok is always true here.
		me, _ := mountEntry(plug, slot, w)
		if err := spec.AddMountEntry(me); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	registerIface(&contentInterface{})
}

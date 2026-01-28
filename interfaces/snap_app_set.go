package interfaces

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

// SnapAppSet is a helper that provides information about executable elements of
// a snap. This currently includes snap apps and hooks.
type SnapAppSet struct {
	info       *snap.Info
	components []*snap.ComponentInfo
}

// NewSnapAppSet returns a new SnapAppSet for the given snap.Info.
func NewSnapAppSet(info *snap.Info, components []*snap.ComponentInfo) (*SnapAppSet, error) {
	for _, c := range components {
		if c.Component.SnapName != info.SnapName() {
			return nil, fmt.Errorf("internal error: snap %q does not own component %q", info.SnapName(), c.Component)
		}
	}
	return &SnapAppSet{info: info, components: components}, nil
}

// Info returns the snap.Info that this SnapAppSet is based on.
func (a *SnapAppSet) Info() *snap.Info {
	return a.info
}

// Components returns the components that this SnapAppSet was created with.
func (a *SnapAppSet) Components() []*snap.ComponentInfo {
	return a.components
}

// InstanceName returns the instance name of the snap that this SnapAppSet is
// based on.
func (a *SnapAppSet) InstanceName() string {
	return a.info.InstanceName()
}

// ExpandSliceSnapVariablesInRootfs resolves $SNAP, $SNAP_DATA, $SNAP_COMMON
// and $SNAP_COMPONENT(<component>) for paths, using the rootfs as mount
// namespace. Paths for components not installed are filtered out.
func (a *SnapAppSet) ExpandSliceSnapVariablesInRootfs(paths []string) []string {
	pathsWithDirIdx := a.ExpandSliceSnapVariablesWithOrder(paths)
	expanded := make([]string, 0, len(pathsWithDirIdx))
	for _, pwi := range pathsWithDirIdx {
		expanded = append(expanded, pwi.Path)
	}
	return expanded
}

// ExpandedDirWithIdx returns the path of an expanded directory and the index in the input
// slice.
type ExpandedDirWithIdx struct {
	Path string
	Idx  int
}

// ExpandSliceSnapVariablesInRootfs resolves $SNAP, $SNAP_DATA, $SNAP_COMMON and
// $SNAP_COMPONENT(<component>) for paths, using the rootfs as mount namespace. Paths for
// components not installed are filtered out. The order specified in paths is returned in the
// output.
func (a *SnapAppSet) ExpandSliceSnapVariablesWithOrder(paths []string) []ExpandedDirWithIdx {
	const componentPrefix = "$SNAP_COMPONENT("

	expandedDirs := make([]ExpandedDirWithIdx, 0, len(paths))
	for idx, dir := range paths {
		if strings.HasPrefix(dir, componentPrefix) {
			compAndPath := strings.SplitN(dir[len(componentPrefix):], ")/", 2)
			if len(compAndPath) != 2 {
				// Should not really happen as these paths are
				// validated by validateSourceDirs
				logger.Noticef("WARNING: wrongly formatted component path: %s",
					componentPrefix)
				continue
			}
			var ci *snap.ComponentInfo
			for _, comp := range a.components {
				if comp.Component.ComponentName == compAndPath[0] {
					ci = comp
					break
				}
			}
			if ci == nil {
				// Component not installed, so we filter out (note however that
				// the presence of the path influences the directory index
				// which might be used later to set the file priority).
				continue
			}
			cpi := snap.MinimalComponentContainerPlaceInfo(
				ci.Component.ComponentName, ci.Revision, a.info.SnapName())
			expandedDirs = append(expandedDirs, ExpandedDirWithIdx{Path: filepath.Clean(
				filepath.Join(cpi.MountDir(), compAndPath[1])),
				Idx: idx,
			})
		} else {
			expandedDirs = append(expandedDirs, ExpandedDirWithIdx{Path: filepath.Clean(
				a.info.ExpandSnapVariablesSetSnapMountDir(
					filepath.Join(dirs.GlobalRootDir, dir),
					dirs.StripRootDir(dirs.SnapMountDir))),
				Idx: idx,
			})
		}
	}
	return expandedDirs
}

// SecurityTagsForConnectedPlug returns the security tags for the given plug.
// These are derived from the security tags of the apps and hooks that are
// associated with the plug.
func (a *SnapAppSet) SecurityTagsForConnectedPlug(plug *ConnectedPlug) ([]string, error) {
	return a.SecurityTagsForPlug(plug.plugInfo)
}

// SecurityTagsForPlug returns the security tags for the given plug. These are
// derived from the security tags of the apps and hooks that are associated with
// the plug.
func (a *SnapAppSet) SecurityTagsForPlug(plug *snap.PlugInfo) ([]string, error) {
	if plug.Snap.InstanceName() != a.info.InstanceName() {
		return nil, fmt.Errorf("internal error: plug %q is from snap %q, security tags can only be computed for processed target snap: %q", plug.Name, plug.Snap.InstanceName(), a.info.InstanceName())
	}

	apps := a.info.AppsForPlug(plug)
	hooks := a.info.HooksForPlug(plug)

	for _, component := range a.components {
		hooks = append(hooks, component.HooksForPlug(plug)...)
	}

	tags := make([]string, 0, len(apps)+len(hooks))
	for _, app := range apps {
		tags = append(tags, app.SecurityTag())
	}

	for _, hook := range hooks {
		tags = append(tags, hook.SecurityTag())
	}

	sort.Strings(tags)

	return tags, nil
}

// SecurityTagsForConnectedSlot returns the security tags for the given slot. These
// are derived from the security tags of the apps and hooks that are associated
// with the slot.
func (a *SnapAppSet) SecurityTagsForConnectedSlot(slot *ConnectedSlot) ([]string, error) {
	return a.SecurityTagsForSlot(slot.slotInfo)
}

// SecurityTagsForSlot returns the security tags for the given slot. These are
// derived from the security tags of the apps and hooks that are associated with
// the slot.
func (a *SnapAppSet) SecurityTagsForSlot(slot *snap.SlotInfo) ([]string, error) {
	if slot.Snap.InstanceName() != a.info.InstanceName() {
		return nil, fmt.Errorf("internal error: slot %q is from snap %q, security tags can only be computed for processed target snap: %q", slot.Name, slot.Snap.InstanceName(), a.info.InstanceName())
	}

	apps := a.info.AppsForSlot(slot)
	hooks := a.info.HooksForSlot(slot)

	tags := make([]string, 0, len(apps)+len(hooks))
	for _, app := range apps {
		tags = append(tags, app.SecurityTag())
	}

	for _, hook := range hooks {
		tags = append(tags, hook.SecurityTag())
	}

	sort.Strings(tags)

	return tags, nil
}

// Runnables returns a list of all runnables known by the app set.
func (a *SnapAppSet) Runnables() []snap.Runnable {
	var runnables []snap.Runnable

	for _, app := range a.info.Apps {
		runnables = append(runnables, app.Runnable())
	}

	for _, hook := range a.info.Hooks {
		runnables = append(runnables, hook.Runnable())
	}

	for _, component := range a.components {
		for _, hook := range component.Hooks {
			runnables = append(runnables, hook.Runnable())
		}
	}

	return runnables
}

// labelExpr returns the specification of the apparmor label describing the
// given connected plug/slot. The result has one of three forms, depending on
// how apps are bound to the slot:
//
//   - "snap.$snap_instance.$app" if there is exactly one app/hook bound
//   - "snap.$snap_instance.{$app1,...$appN, $hook1...$hookN}" if there are
//     some, but not all, apps/hooks bound
//   - "snap.$snap_instance.*" if all apps/hooks are bound to the plug or slot
func labelExpr(connected interface {
	AppSet() *SnapAppSet
	Runnables() []snap.Runnable
}) string {
	appSet := connected.AppSet()
	runnables := connected.Runnables()

	// all security tags are prefixed with snap.$snap_instance, we use this
	// knowledge to build a pattern that will match against all of the connected
	// runnables
	prefix := fmt.Sprintf("snap.%s", appSet.InstanceName())

	suffixes := make([]string, 0, len(runnables))
	for _, r := range runnables {
		suffix := strings.TrimPrefix(r.SecurityTag, prefix)
		if suffix == r.SecurityTag {
			logger.Noticef("security tag %q does not have expected prefix: %q", r.SecurityTag, prefix)
			continue
		}
		suffixes = append(suffixes, suffix)
	}

	sort.Strings(suffixes)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `"%s`, prefix)

	switch len(suffixes) {
	case 0:
		buf.WriteString(".")
	case 1:
		buf.WriteString(suffixes[0])
	case len(appSet.Runnables()):
		buf.WriteString(".*")
	default:
		buf.WriteByte('{')
		for _, name := range suffixes {
			buf.WriteString(name)
			buf.WriteByte(',')
		}
		// remove trailing comma
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	}

	buf.WriteByte('"')

	return buf.String()
}

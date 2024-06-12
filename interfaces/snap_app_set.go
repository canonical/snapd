package interfaces

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

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

// InstanceName returns the instance name of the snap that this SnapAppSet is
// based on.
func (a *SnapAppSet) InstanceName() string {
	return a.info.InstanceName()
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

// plugLabelExpression returns the label expression for the given plug. It is
// constructed from the apps and hooks that are associated with the plug.
func (a *SnapAppSet) plugLabelExpression(plug *ConnectedPlug) string {
	if a.info.InstanceName() != plug.appSet.InstanceName() {
		panic("internal error: connected plug must be from the same snap as the SnapAppSet")
	}

	return labelExpr(a, a.PlugRunnables(plug))
}

// slotLabelExpression returns the label expression for the given slot. It is
// constructed from the apps and hooks that are associated with the slot.
func (a *SnapAppSet) slotLabelExpression(slot *ConnectedSlot) string {
	if a.info.InstanceName() != slot.appSet.InstanceName() {
		panic("internal error: connected slot must be from the same snap as the SnapAppSet")
	}

	return labelExpr(a, a.SlotRunnables(slot))
}

// Runnables returns a list of all runnables known by the app set.
func (a *SnapAppSet) Runnables() []snap.Runnable {
	var runnables []snap.Runnable

	for _, app := range a.info.Apps {
		runnables = append(runnables, snap.AppRunnable(app))
	}

	for _, hook := range a.info.Hooks {
		runnables = append(runnables, snap.HookRunnable(hook))
	}

	for _, component := range a.components {
		for _, hook := range component.Hooks {
			runnables = append(runnables, snap.HookRunnable(hook))
		}
	}

	return runnables
}

// PlugRunnables returns a list of all runnables that should be connected to the
// given plug.
func (a *SnapAppSet) PlugRunnables(plug *ConnectedPlug) []snap.Runnable {
	apps := a.info.AppsForPlug(plug.plugInfo)
	hooks := a.info.HooksForPlug(plug.plugInfo)
	for _, component := range a.components {
		hooks = append(hooks, component.HooksForPlug(plug.plugInfo)...)
	}

	return appAndHookRunnables(apps, hooks)
}

// SlotRunnables returns a list of all runnables that should be connected to the
// given slot.
func (a *SnapAppSet) SlotRunnables(slot *ConnectedSlot) []snap.Runnable {
	apps := a.info.AppsForSlot(slot.slotInfo)
	hooks := a.info.HooksForSlot(slot.slotInfo)

	// TODO: if components ever get slots, they will need to be considered here

	return appAndHookRunnables(apps, hooks)
}

func appAndHookRunnables(apps []*snap.AppInfo, hooks []*snap.HookInfo) []snap.Runnable {
	runnables := make([]snap.Runnable, 0, len(apps)+len(hooks))
	for _, app := range apps {
		runnables = append(runnables, snap.AppRunnable(app))
	}

	for _, hook := range hooks {
		runnables = append(runnables, snap.HookRunnable(hook))
	}

	return runnables
}

// labelExpr returns the specification of the apparmor label describing given
// apps and hooks. The result has one of three forms, depending on how apps are
// bound to the slot:
//
//   - "snap.$snap_instance.$app" if there is exactly one app/hook bound
//   - "snap.$snap_instance.{$app1,...$appN, $hook1...$hookN}" if there are
//     some, but not all, apps/hooks bound
//   - "snap.$snap_instance.*" if all apps/hooks are bound to the plug or slot
func labelExpr(appSet *SnapAppSet, connected []snap.Runnable) string {
	var buf bytes.Buffer

	// all security tags are prefixed with snap.$snap_instance, we use this
	// knowledge to build a pattern that will match against all of the connected
	// runnables
	prefix := fmt.Sprintf("snap.%s", appSet.InstanceName())

	suffixes := make([]string, 0, len(connected))
	for _, r := range connected {
		suffixes = append(suffixes, strings.TrimPrefix(r.SecurityTag, prefix))
	}

	sort.Strings(suffixes)

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

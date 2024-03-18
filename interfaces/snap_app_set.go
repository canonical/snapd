package interfaces

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/snap"
)

// SnapAppSet is a helper that provides information about executable elements of
// a snap. This currently includes snap apps and hooks.
// TODO: include component hooks when they are implemented
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

// PlugLabelExpression returns the label expression for the given plug. It is
// constructed from the apps and hooks that are associated with the plug.
func (a *SnapAppSet) PlugLabelExpression(plug *ConnectedPlug) string {
	// TODO: this is a hack that will not continue to work once component hooks
	// are introduced. the methods on SnapAppSet should only be called on
	// slots/hooks that originated from the snap that the SnapAppSet was derived
	// from.
	info := a.info
	if a.info.InstanceName() != plug.plugInfo.Snap.InstanceName() {
		info = plug.plugInfo.Snap
	}

	apps := info.AppsForPlug(plug.plugInfo)
	hooks := info.HooksForPlug(plug.plugInfo)
	return labelExpr(apps, hooks, info)
}

// SlotLabelExpression returns the label expression for the given slot. It is
// constructed from the apps and hooks that are associated with the slot.
func (a *SnapAppSet) SlotLabelExpression(slot *ConnectedSlot) string {
	// TODO: this is a hack that will not continue to work once component hooks
	// are introduced. the methods on SnapAppSet should only be called on
	// slots/hooks that originated from the snap that the SnapAppSet was derived
	// from.
	info := a.info
	if a.info.InstanceName() != slot.slotInfo.Snap.InstanceName() {
		info = slot.slotInfo.Snap
	}

	apps := info.AppsForSlot(slot.slotInfo)
	hooks := info.HooksForSlot(slot.slotInfo)
	return labelExpr(apps, hooks, info)
}

// labelExpr returns the specification of the apparmor label describing given
// apps and hooks. The result has one of three forms, depending on how apps are
// bound to the slot:
//
//   - "snap.$snap_instance.$app" if there is exactly one app/hook bound
//   - "snap.$snap_instance.{$app1,...$appN, $hook1...$hookN}" if there are
//     some, but not all, apps/hooks bound
//   - "snap.$snap_instance.*" if all apps/hooks are bound to the plug or slot
func labelExpr(apps []*snap.AppInfo, hooks []*snap.HookInfo, snap *snap.Info) string {
	var buf bytes.Buffer

	names := make([]string, 0, len(apps)+len(hooks))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	for _, hook := range hooks {
		names = append(names, fmt.Sprintf("hook.%s", hook.Name))
	}
	sort.Strings(names)

	fmt.Fprintf(&buf, `"snap.%s.`, snap.InstanceName())
	if len(names) == 1 {
		buf.WriteString(names[0])
	} else if len(apps) == len(snap.Apps) && len(hooks) == len(snap.Hooks) {
		buf.WriteByte('*')
	} else if len(names) > 0 {
		buf.WriteByte('{')
		for _, name := range names {
			buf.WriteString(name)
			buf.WriteByte(',')
		}
		// remove trailing comma
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	} // else: len(names)==0, gives "snap.<name>." that doesn't match anything
	buf.WriteByte('"')
	return buf.String()
}

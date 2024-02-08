package interfaces

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/snap"
)

type SnapAppSet struct {
	info *snap.Info
}

func NewSnapAppSet(info *snap.Info) *SnapAppSet {
	return &SnapAppSet{info: info}
}

func (a *SnapAppSet) SecurityTagsForConnectedPlug(plug *ConnectedPlug) []string {
	return a.SecurityTagsForPlug(plug.plugInfo)
}

func (a *SnapAppSet) SecurityTagsForPlug(plug *snap.PlugInfo) []string {
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

	return tags
}

func (a *SnapAppSet) SecurityTagsForConnectedSlot(slot *ConnectedSlot) []string {
	return a.SecurityTagsForSlot(slot.slotInfo)
}

func (a *SnapAppSet) SecurityTagsForSlot(slot *snap.SlotInfo) []string {
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

	return tags
}

func (a *SnapAppSet) PlugLabelExpression(plug *ConnectedPlug) string {
	apps := a.info.AppsForPlug(plug.plugInfo)
	hooks := a.info.HooksForPlug(plug.plugInfo)
	return labelExpr(apps, hooks, plug.Snap())
}

func (a *SnapAppSet) SlotLabelExpression(slot *ConnectedSlot) string {
	apps := a.info.AppsForSlot(slot.slotInfo)
	hooks := a.info.HooksForSlot(slot.slotInfo)
	return labelExpr(apps, hooks, slot.Snap())
}

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

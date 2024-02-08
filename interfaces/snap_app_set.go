package interfaces

import (
	"github.com/snapcore/snapd/snap"
)

type SnapAppSet struct {
	info *snap.Info
}

func NewSnapAppSet(info *snap.Info) *SnapAppSet {
	return &SnapAppSet{info: info}
}

func (a *SnapAppSet) SecurityTagsForConnectedPlug(plug *ConnectedPlug) []string {
	return nil
}

func (a *SnapAppSet) SecurityTagsForPlug(plug *snap.PlugInfo) []string {
	return nil
}

func (a *SnapAppSet) SecurityTagsForConnectedSlot(slot *ConnectedSlot) []string {
	return nil
}

func (a *SnapAppSet) SecurityTagsForSlot(slot *snap.SlotInfo) []string {
	return nil
}

func (a *SnapAppSet) PlugLabelExpression(plug *ConnectedPlug) string {
	return ""
}

func (a *SnapAppSet) SlotLabelExpression(slot *ConnectedSlot) string {
	return ""
}

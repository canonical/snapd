package ifacetest

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"gopkg.in/check.v1"
)

func MockInfoAndAppSet(c *check.C, yamlText string, componentYamls []string, sideInfo *snap.SideInfo) *interfaces.SnapAppSet {
	info := snaptest.MockInfo(c, yamlText, sideInfo)
	return mockAppSet(c, componentYamls, info)
}

func MockInstanceAndAppSet(c *check.C, instanceName string, yamlText string, componentYamls []string, sideInfo *snap.SideInfo) *interfaces.SnapAppSet {
	info := snaptest.MockSnapInstance(c, instanceName, yamlText, sideInfo)
	return mockAppSet(c, componentYamls, info)
}

func MockSnapAndAppSet(c *check.C, yamlText string, componentYamls []string, sideInfo *snap.SideInfo) *interfaces.SnapAppSet {
	info := snaptest.MockSnap(c, yamlText, sideInfo)
	return mockAppSet(c, componentYamls, info)
}

func mockAppSet(c *check.C, componentYamls []string, info *snap.Info) *interfaces.SnapAppSet {
	components := make([]*snap.ComponentInfo, 0, len(componentYamls))
	for _, yaml := range componentYamls {
		components = append(components, snaptest.MockComponent(c, yaml, info, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	set, err := interfaces.NewSnapAppSet(info, components)
	c.Assert(err, check.IsNil)

	return set
}

func MockProviderYaml(snapName, plugName string) string {
	const template = `name: %s
version: 1
plugs:
 %[2]s:
  interface: %[2]s
`
	return fmt.Sprintf(template, snapName, plugName)
}

func MockConnectedPlug(c *check.C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	return MockConnectedPlugWithAttrs(c, yaml, si, plugName, nil, nil)
}

func MockConnectedSlot(c *check.C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	return MockConnectedSlotWithAttrs(c, yaml, si, slotName, nil, nil)
}

func MockConnectedSlotWithAttrs(c *check.C, yaml string, si *snap.SideInfo, slotName string, staticAttrs, dynamicAttrs map[string]interface{}) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, check.IsNil)

	slotInfo, ok := info.Slots[slotName]
	if !ok {
		c.Fatalf("cannot find slot %q in snap %q", slotName, info.InstanceName())
	}

	return interfaces.NewConnectedSlot(slotInfo, set, staticAttrs, dynamicAttrs), slotInfo
}

func MockConnectedPlugWithAttrs(c *check.C, yaml string, si *snap.SideInfo, plugName string, staticAttrs, dynamicAttrs map[string]interface{}) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, check.IsNil)

	plugInfo, ok := info.Plugs[plugName]
	if !ok {
		c.Fatalf("cannot find plug %q in snap %q", plugName, info.InstanceName())
	}

	return interfaces.NewConnectedPlug(plugInfo, set, staticAttrs, dynamicAttrs), plugInfo
}

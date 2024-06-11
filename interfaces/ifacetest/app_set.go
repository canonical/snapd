package ifacetest

import (
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

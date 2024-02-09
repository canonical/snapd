package interfaces_test

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	. "gopkg.in/check.v1"
)

type snapAppSetSuite struct{}

var _ = Suite(&snapAppSetSuite{})

const yaml = `name: test-snap
version: 1
plugs:
  x11:
slots:
  opengl:
apps:
  app1:
    command: bin/test1
    plugs: [home]
    slots: [unity8]
  app2:
    command: bin/test2
    plugs: [home]
hooks:
  install:
    plugs: [network, network-manager]
  post-refresh:
    plugs: [network, network-manager]
`

func (s *snapAppSetSuite) TestPlugLabelExpr(c *C) {
	info, connectedPlug := mockInfoAndConnectedPlug(c, yaml, nil, "network")
	set := interfaces.NewSnapAppSet(info)

	label := set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.{hook.install,hook.post-refresh}"`)

	info, connectedPlug = mockInfoAndConnectedPlug(c, yaml, nil, "home")
	set = interfaces.NewSnapAppSet(info)

	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.{app1,app2}"`)

	info, connectedPlug = mockInfoAndConnectedPlug(c, yaml, nil, "x11")
	set = interfaces.NewSnapAppSet(info)

	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestSlotLabelExpr(c *C) {
	info, connectedSlot := mockInfoAndConnectedSlot(c, yaml, nil, "unity8")
	set := interfaces.NewSnapAppSet(info)

	label := set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	info, connectedSlot = mockInfoAndConnectedSlot(c, yaml, nil, "opengl")
	set = interfaces.NewSnapAppSet(info)

	label = set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestLabelExpr(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)

	apps := appsInMap(info.Apps)
	hooks := hooksInMap(info.Hooks)

	// all apps and all hooks
	label := interfaces.LabelExpr(apps, hooks, info)
	c.Check(label, Equals, `"snap.test-snap.*"`)

	// all apps, no hooks
	label = interfaces.LabelExpr(apps, nil, info)
	c.Check(label, Equals, `"snap.test-snap.{app1,app2}"`)

	// one app, no hooks
	label = interfaces.LabelExpr([]*snap.AppInfo{info.Apps["app1"]}, nil, info)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	// no apps, one hook
	label = interfaces.LabelExpr(nil, []*snap.HookInfo{info.Hooks["install"]}, info)
	c.Check(label, Equals, `"snap.test-snap.hook.install"`)

	// one app, all hooks
	label = interfaces.LabelExpr([]*snap.AppInfo{info.Apps["app1"]}, hooks, info)
	c.Check(label, Equals, `"snap.test-snap.{app1,hook.install,hook.post-refresh}"`)

	// only hooks
	label = interfaces.LabelExpr(nil, hooks, info)
	c.Check(label, Equals, `"snap.test-snap.{hook.install,hook.post-refresh}"`)

	// nothing
	label = interfaces.LabelExpr(nil, nil, info)
	c.Check(label, Equals, `"snap.test-snap."`)
}

func appsInMap(apps map[string]*snap.AppInfo) []*snap.AppInfo {
	result := make([]*snap.AppInfo, 0, len(apps))
	for _, app := range apps {
		result = append(result, app)
	}
	return result
}

func hooksInMap(hooks map[string]*snap.HookInfo) []*snap.HookInfo {
	result := make([]*snap.HookInfo, 0, len(hooks))
	for _, hook := range hooks {
		result = append(result, hook)
	}
	return result
}

func mockInfoAndConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*snap.Info, *interfaces.ConnectedPlug) {
	info := snaptest.MockInfo(c, yaml, si)
	plugInfo, ok := info.Plugs[plugName]
	if !ok {
		c.Fatalf("cannot find plug %q in snap %q", plugName, info.InstanceName())
	}
	return info, interfaces.NewConnectedPlug(plugInfo, nil, nil)
}

func mockInfoAndConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*snap.Info, *interfaces.ConnectedSlot) {
	info := snaptest.MockInfo(c, yaml, si)
	slotInfo, ok := info.Slots[slotName]
	if !ok {
		c.Fatalf("cannot find slot %q in snap %q", slotName, info.InstanceName())
	}
	return info, interfaces.NewConnectedSlot(slotInfo, nil, nil)
}

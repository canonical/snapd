package interfaces_test

import (
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type snapAppSetSuite struct {
	testutil.BaseTest
}

var _ = Suite(&snapAppSetSuite{})

func (s *snapAppSetSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

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
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	label := set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.{hook.install,hook.post-refresh}"`)

	info, connectedPlug = mockInfoAndConnectedPlug(c, yaml, nil, "home")
	set, err = interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.{app1,app2}"`)

	info, connectedPlug = mockInfoAndConnectedPlug(c, yaml, nil, "x11")
	set, err = interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestPlugLabelExprInfoFallback(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	const otherInfo = `name: other-name
version: 1
apps:
  app1:
  app2:
hooks:
  install:
plugs:
  plug:
slots:
  slot:`

	_, connectedPlug := mockInfoAndConnectedPlug(c, otherInfo, nil, "plug")

	label := set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.other-name.*"`)
}

func (s *snapAppSetSuite) TestSlotLabelExpr(c *C) {
	info, connectedSlot := mockInfoAndConnectedSlot(c, yaml, nil, "unity8")
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	label := set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	info, connectedSlot = mockInfoAndConnectedSlot(c, yaml, nil, "opengl")
	set, err = interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	label = set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestSlotLabelExprInfoFallback(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	const otherInfo = `name: other-name
version: 1
apps:
  app1:
  app2:
hooks:
  install:
plugs:
  plug:
slots:
  slot:`

	_, connectedSlot := mockInfoAndConnectedSlot(c, otherInfo, nil, "slot")

	label := set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.other-name.*"`)
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

func (s *snapAppSetSuite) TestPlugSecurityTags(c *C) {
	const yaml = `name: name
version: 1
apps:
  app1:
  app2:
components:
  comp:
    type: test
    hooks:
      install:
hooks:
  install:
plugs:
  plug:
slots:
  slot:
components:
  comp:
    type: test`
	info, connectedPlug := mockInfoAndConnectedPlug(c, yaml, nil, "plug")
	compInfo := snaptest.MockComponent(c, "component: name+comp\ntype: test\nversion: 1", info)

	set, err := interfaces.NewSnapAppSet(info, []*snap.ComponentInfo{compInfo})
	c.Assert(err, IsNil)

	tags, err := set.SecurityTagsForConnectedPlug(connectedPlug)
	c.Assert(err, IsNil)
	c.Assert(tags, DeepEquals, []string{"snap.name.app1", "snap.name.app2", "snap.name.hook.install"})
}

func (s *snapAppSetSuite) TestPlugSecurityTagsWrongSnap(c *C) {
	const yaml = `name: name
version: 1`
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	const otherYaml = `name: other-name
version: 1
plugs:
  plug:`
	_, connectedPlug := mockInfoAndConnectedPlug(c, otherYaml, nil, "plug")

	_, err = set.SecurityTagsForConnectedPlug(connectedPlug)
	c.Assert(err, ErrorMatches, `internal error: plug "plug" is from snap "other-name", security tags can only be computed for processed target snap: "name"`)
}

func (s *snapAppSetSuite) TestSlotSecurityTags(c *C) {
	const yaml = `name: name
version: 1
apps:
  app1:
  app2:
hooks:
  install:
plugs:
  plug:
slots:
  slot:`
	info, connectedSlot := mockInfoAndConnectedSlot(c, yaml, nil, "slot")
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	tags, err := set.SecurityTagsForConnectedSlot(connectedSlot)
	c.Assert(err, IsNil)
	c.Assert(tags, DeepEquals, []string{"snap.name.app1", "snap.name.app2", "snap.name.hook.install"})
}

func (s *snapAppSetSuite) TestSlotSecurityTagsWrongSnap(c *C) {
	const yaml = `name: name
version: 1`
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	const otherYaml = `name: other-name
version: 1
slots:
  slot:`
	_, connectedSlot := mockInfoAndConnectedSlot(c, otherYaml, nil, "slot")

	_, err = set.SecurityTagsForConnectedSlot(connectedSlot)
	c.Assert(err, ErrorMatches, `internal error: slot "slot" is from snap "other-name", security tags can only be computed for processed target snap: "name"`)
}

func (s *snapAppSetSuite) TestRunnables(c *C) {
	const yaml = `name: name
version: 1
apps:
  app1:
  app2:
hooks:
  install:
components:
  comp:
    type: test
    hooks:
      install:
`
	info := snaptest.MockInfo(c, yaml, nil)

	compInfo := snaptest.MockComponent(c, "component: name+comp\ntype: test\nversion: 1.0", info)

	set, err := interfaces.NewSnapAppSet(info, []*snap.ComponentInfo{compInfo})
	c.Assert(err, IsNil)

	c.Check(set.Runnables(), DeepEquals, []interfaces.Runnable{
		{
			CommandName: "app1",
			SecurityTag: "snap.name.app1",
			Type:        interfaces.RunnableApp,
		},
		{
			CommandName: "app2",
			SecurityTag: "snap.name.app2",
			Type:        interfaces.RunnableApp,
		},
		{
			CommandName: "hook.install",
			SecurityTag: "snap.name.hook.install",
			Type:        interfaces.RunnableHook,
		},
		{
			CommandName: "name+comp.hook.install",
			SecurityTag: "snap.name+comp.hook.install",
			Type:        interfaces.RunnableComponentHook,
		},
	})
}

func (s *snapAppSetSuite) TestInfo(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	c.Check(set.Info(), DeepEquals, info)
}

func (s *snapAppSetSuite) TestInstanceName(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)
	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	c.Check(set.InstanceName(), Equals, "test-snap")
}

func (s *snapAppSetSuite) TestNewAppSetWithWrongComponent(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)
	_, err := interfaces.NewSnapAppSet(info, []*snap.ComponentInfo{{
		Component: naming.NewComponentRef("other-name", "comp"),
	}})
	c.Assert(err, ErrorMatches, `internal error: snap "test-snap" does not own component "other-name\+comp"`)
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

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
components:
  comp1:
    type: test
    hooks:
      install:
  comp2:
    type: test
    hooks:
      pre-refresh:
`

func (s *snapAppSetSuite) TestPlugLabelExpr(c *C) {
	set, connectedPlug := mockAppSetAndConnectedPlug(c, yaml, nil, nil, "network")
	label := set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap{.hook.install,.hook.post-refresh}"`)

	set, connectedPlug = mockAppSetAndConnectedPlug(c, yaml, nil, nil, "home")
	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap{.app1,.app2}"`)

	set, connectedPlug = mockAppSetAndConnectedPlug(c, yaml, nil, nil, "x11")
	label = set.PlugLabelExpression(connectedPlug)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestSlotLabelExpr(c *C) {
	set, connectedSlot := mockAppSetAndConnectedSlot(c, yaml, nil, nil, "unity8")

	label := set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	set, connectedSlot = mockAppSetAndConnectedSlot(c, yaml, nil, nil, "opengl")

	label = set.SlotLabelExpression(connectedSlot)
	c.Check(label, Equals, `"snap.test-snap.*"`)
}

func (s *snapAppSetSuite) TestLabelExpr(c *C) {
	info := snaptest.MockInfo(c, yaml, nil)

	compYamls := []string{
		"component: test-snap+comp1\ntype: test",
		"component: test-snap+comp2\ntype: test",
	}

	compInfos := make([]*snap.ComponentInfo, 0, len(compYamls))
	for _, compYaml := range compYamls {
		compInfos = append(compInfos, snaptest.MockComponent(c, compYaml, info, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	appSet, err := interfaces.NewSnapAppSet(info, compInfos)
	c.Assert(err, IsNil)

	apps := appsInMap(info.Apps)
	hooks := hooksInMap(info.Hooks)

	var compHooks []*snap.HookInfo
	for _, ci := range compInfos {
		compHooks = append(compHooks, hooksInMap(ci.Hooks)...)
	}

	allHooks := make([]*snap.HookInfo, 0, len(hooks)+len(compHooks))
	allHooks = append(allHooks, hooks...)
	allHooks = append(allHooks, compHooks...)

	// all apps and all hooks
	label := interfaces.LabelExpr(apps, allHooks, appSet)
	c.Check(label, Equals, `"snap.test-snap.*"`)

	// all apps, no hooks
	label = interfaces.LabelExpr(apps, nil, appSet)
	c.Check(label, Equals, `"snap.test-snap{.app1,.app2}"`)

	// one app, no hooks
	label = interfaces.LabelExpr([]*snap.AppInfo{info.Apps["app1"]}, nil, appSet)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	// no apps, one snap hook
	label = interfaces.LabelExpr(nil, []*snap.HookInfo{info.Hooks["install"]}, appSet)
	c.Check(label, Equals, `"snap.test-snap.hook.install"`)

	// one app, all snap hooks
	label = interfaces.LabelExpr([]*snap.AppInfo{info.Apps["app1"]}, hooks, appSet)
	c.Check(label, Equals, `"snap.test-snap{.app1,.hook.install,.hook.post-refresh}"`)

	// one app, all hooks
	label = interfaces.LabelExpr([]*snap.AppInfo{info.Apps["app1"]}, allHooks, appSet)
	c.Check(label, Equals, `"snap.test-snap{+comp1.hook.install,+comp2.hook.pre-refresh,.app1,.hook.install,.hook.post-refresh}"`)

	// only snap hooks
	label = interfaces.LabelExpr(nil, hooks, appSet)
	c.Check(label, Equals, `"snap.test-snap{.hook.install,.hook.post-refresh}"`)

	// only component hooks
	label = interfaces.LabelExpr(nil, compHooks, appSet)
	c.Check(label, Equals, `"snap.test-snap{+comp1.hook.install,+comp2.hook.pre-refresh}"`)

	// nothing
	label = interfaces.LabelExpr(nil, nil, appSet)
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
  slot:`

	set, connectedPlug := mockAppSetAndConnectedPlug(c, yaml, []string{
		"component: name+comp\ntype: test\nversion: 1",
	}, nil, "plug")

	tags, err := set.SecurityTagsForConnectedPlug(connectedPlug)
	c.Assert(err, IsNil)
	c.Assert(tags, DeepEquals, []string{
		"snap.name+comp.hook.install",
		"snap.name.app1",
		"snap.name.app2",
		"snap.name.hook.install",
	})
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
	_, connectedPlug := mockAppSetAndConnectedPlug(c, otherYaml, nil, nil, "plug")

	_, err = set.SecurityTagsForConnectedPlug(connectedPlug)
	c.Assert(err, ErrorMatches, `internal error: plug "plug" is from snap "other-name", security tags can only be computed for processed target snap: "name"`)
}

func (s *snapAppSetSuite) TestSlotSecurityTags(c *C) {
	const yaml = `name: name
version: 1
apps:
  app1:
  app2:
components:
  comp:
    hooks:
      install:
hooks:
  install:
plugs:
  plug:
slots:
  slot:`
	set, connectedSlot := mockAppSetAndConnectedSlot(c, yaml, nil, nil, "slot")

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
	_, connectedSlot := mockAppSetAndConnectedSlot(c, otherYaml, nil, nil, "slot")

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

	compInfo := snaptest.MockComponent(c, "component: name+comp\ntype: test\nversion: 1.0", info, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	set, err := interfaces.NewSnapAppSet(info, []*snap.ComponentInfo{compInfo})
	c.Assert(err, IsNil)

	c.Check(set.Runnables(), testutil.DeepUnsortedMatches, []snap.Runnable{
		{
			CommandName: "app1",
			SecurityTag: "snap.name.app1",
		},
		{
			CommandName: "app2",
			SecurityTag: "snap.name.app2",
		},
		{
			CommandName: "hook.install",
			SecurityTag: "snap.name.hook.install",
		},
		{
			CommandName: "name+comp.hook.install",
			SecurityTag: "snap.name+comp.hook.install",
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
	_, err := interfaces.NewSnapAppSet(info, []*snap.ComponentInfo{
		snap.NewComponentInfo(naming.NewComponentRef("other-name", "comp"), snap.TestComponent, "", "", "", "", nil),
	})
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

func mockAppSetAndConnectedPlug(c *C, yaml string, compYamls []string, si *snap.SideInfo, plugName string) (*interfaces.SnapAppSet, *interfaces.ConnectedPlug) {
	info := snaptest.MockInfo(c, yaml, si)

	compInfos := make([]*snap.ComponentInfo, 0, len(compYamls))
	for _, compYaml := range compYamls {
		compInfos = append(compInfos, snaptest.MockComponent(c, compYaml, info, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	appSet, err := interfaces.NewSnapAppSet(info, compInfos)
	c.Assert(err, IsNil)

	plugInfo, ok := info.Plugs[plugName]
	if !ok {
		c.Fatalf("cannot find plug %q in snap %q", plugName, info.InstanceName())
	}

	return appSet, interfaces.NewConnectedPlug(plugInfo, appSet, nil, nil)
}

func mockAppSet(c *C, yaml string, compYamls []string, si *snap.SideInfo) *interfaces.SnapAppSet {
	info := snaptest.MockInfo(c, yaml, si)

	compInfos := make([]*snap.ComponentInfo, 0, len(compYamls))
	for _, compYaml := range compYamls {
		compInfos = append(compInfos, snaptest.MockComponent(c, compYaml, info, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	appSet, err := interfaces.NewSnapAppSet(info, compInfos)
	c.Assert(err, IsNil)

	return appSet
}

func mockAppSetAndConnectedSlot(c *C, yaml string, compYamls []string, si *snap.SideInfo, slotName string) (*interfaces.SnapAppSet, *interfaces.ConnectedSlot) {
	appSet := mockAppSet(c, yaml, compYamls, si)

	slotInfo, ok := appSet.Info().Slots[slotName]
	if !ok {
		c.Fatalf("cannot find slot %q in snap %q", slotName, appSet.InstanceName())
	}

	return appSet, interfaces.NewConnectedSlot(slotInfo, appSet, nil, nil)
}

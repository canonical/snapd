// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package builtin_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	apparmorutils "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	iface        interfaces.Interface
	slotOS       *snap.SlotInfo
	slotApp      *snap.SlotInfo
	slotSnapd    *snap.SlotInfo
	slotGadget   *snap.SlotInfo
	conSlotOS    *interfaces.ConnectedSlot
	conSlotSnapd *interfaces.ConnectedSlot
	conSlotApp   *interfaces.ConnectedSlot
}

func connectedSlotFromInfo(info *snap.Info) *interfaces.ConnectedSlot {
	appSet, err := interfaces.NewSnapAppSet(info, nil)
	if err != nil {
		panic(fmt.Sprintf("cannot create snap app set: %v", err))
	}

	return interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: info}, appSet, nil, nil)
}

var _ = Suite(&utilsSuite{
	iface:        &ifacetest.TestInterface{InterfaceName: "iface"},
	slotOS:       &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeOS}},
	slotApp:      &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeApp}},
	slotSnapd:    &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeSnapd, SuggestedName: "snapd"}},
	slotGadget:   &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeGadget}},
	conSlotOS:    connectedSlotFromInfo(&snap.Info{SnapType: snap.TypeOS, SuggestedName: "core"}),
	conSlotSnapd: connectedSlotFromInfo(&snap.Info{SnapType: snap.TypeSnapd, SuggestedName: "snapd"}),
	conSlotApp:   connectedSlotFromInfo(&snap.Info{SnapType: snap.TypeApp, SuggestedName: "app"}),
})

func (s *utilsSuite) TestIsSlotSystemSlot(c *C) {
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotSnapd), Equals, true)
}

func (s *utilsSuite) TestImplicitSystemConnectedSlot(c *C) {
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotSnapd), Equals, true)
}

func MockPlug(c *C, yaml string, si *snap.SideInfo, plugName string) *snap.PlugInfo {
	return builtin.MockPlug(c, yaml, si, plugName)
}

func MockSlot(c *C, yaml string, si *snap.SideInfo, slotName string) *snap.SlotInfo {
	return builtin.MockSlot(c, yaml, si, slotName)
}

func MockConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if plugInfo, ok := info.Plugs[plugName]; ok {
		return interfaces.NewConnectedPlug(plugInfo, set, nil, nil), plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func MockConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if slotInfo, ok := info.Slots[slotName]; ok {
		return interfaces.NewConnectedSlot(slotInfo, set, nil, nil), slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func MockHotplugSlot(c *C, yaml string, si *snap.SideInfo, hotplugKey snap.HotplugKey, ifaceName, slotName string, staticAttrs map[string]any) *snap.SlotInfo {
	info := snaptest.MockInfo(c, yaml, si)
	if _, ok := info.Slots[slotName]; ok {
		panic(fmt.Sprintf("slot %q already present in the snap yaml", slotName))
	}
	return &snap.SlotInfo{
		Snap:       info,
		Name:       slotName,
		Attrs:      staticAttrs,
		HotplugKey: hotplugKey,
	}
}

func (s *utilsSuite) TestStringListAttributeHappy(c *C) {
	const snapYaml = `name: consumer
version: 0
plugs:
 personal-files:
  write: ["$HOME/dir1", "/etc/.hidden2"]
slots:
 shared-memory:
  write: ["foo", "bar"]
`
	plug, _ := MockConnectedPlug(c, snapYaml, nil, "personal-files")
	slot, _ := MockConnectedSlot(c, snapYaml, nil, "shared-memory")

	list, err := builtin.StringListAttribute(plug, "write")
	c.Assert(err, IsNil)
	c.Check(list, DeepEquals, []string{"$HOME/dir1", "/etc/.hidden2"})
	list, err = builtin.StringListAttribute(plug, "read")
	c.Assert(err, IsNil)
	c.Check(list, IsNil)
	list, err = builtin.StringListAttribute(slot, "write")
	c.Assert(err, IsNil)
	c.Check(list, DeepEquals, []string{"foo", "bar"})
}

func (s *utilsSuite) TestStringListAttributeErrorNotListStrings(c *C) {
	const snapYaml = `name: consumer
version: 0
plugs:
 personal-files:
  write: [1, "two"]
`
	plug, _ := MockConnectedPlug(c, snapYaml, nil, "personal-files")
	list, err := builtin.StringListAttribute(plug, "write")
	c.Assert(list, IsNil)
	c.Assert(err, ErrorMatches, `"write" attribute must be a list of strings, not "\[1 two\]"`)
}

// desktopFileRulesBaseSuite should be extended by interfaces that use getDesktopFileRules()
// like unity7 and desktop-legacy
//
// TODO: Add a way to detect interfaces that use getDesktopFileRules() and don't have an
// instance of this test suite.
type desktopFileRulesBaseSuite struct {
	iface    string
	slotYaml string

	rootDir       string
	fallbackRules []string
}

func (s *desktopFileRulesBaseSuite) SetUpTest(c *C) {
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)

	s.fallbackRules = []string{
		// generic fallback snap desktop rules are generated
		fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", dirs.SnapDesktopFilesDir),
		"# Explicitly deny access to other snap's desktop files",
		fmt.Sprintf("deny %s/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,", dirs.SnapDesktopFilesDir),
	}
}

func (s *desktopFileRulesBaseSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

type testDesktopFileRulesOptions struct {
	snapName       string
	desktopFiles   []string
	desktopFileIDs []string
	isInstance     bool
	expectedRules  []string
	expectedErr    string
}

func (s *desktopFileRulesBaseSuite) testDesktopFileRules(c *C, opts testDesktopFileRulesOptions) {
	iface := builtin.MustInterface(s.iface)

	const mockPlugSnapInfoYamlTemplate = `name: %s
version: 1.0
apps:
 app2:
  command: foo
  plugs:
    - %s
    - desktop
`
	mockPlugSnapInfoYaml := fmt.Sprintf(mockPlugSnapInfoYamlTemplate, opts.snapName, iface.Name())

	if len(opts.desktopFileIDs) > 0 {
		mockPlugSnapInfoYaml += `
plugs:
  desktop:
    desktop-file-ids: [` + strings.Join(opts.desktopFileIDs, ",") + `]
`
	}

	slot, _ := MockConnectedSlot(c, s.slotYaml, nil, iface.Name())
	plug, _ := MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, iface.Name())
	securityTag := "snap." + opts.snapName + ".app2"
	if opts.isInstance {
		plug.AppSet().Info().InstanceKey = "instance"
		securityTag = "snap." + opts.snapName + "_instance.app2"
	}

	// mock snap desktop files under snap mount
	guiDir := filepath.Join(plug.AppSet().Info().MountDir(), "meta", "gui")
	c.Assert(os.MkdirAll(guiDir, 0755), IsNil)
	for _, desktopFile := range opts.desktopFiles {
		c.Assert(os.WriteFile(filepath.Join(guiDir, desktopFile), nil, 0644), IsNil)
	}

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(iface, plug, slot)
	if opts.expectedErr != "" {
		c.Assert(err.Error(), Equals, opts.expectedErr)
		return
	}
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SnippetForTag(securityTag), testutil.Contains, `# This leaks the names of snaps with desktop files`)
	c.Assert(apparmorSpec.SnippetForTag(securityTag), testutil.Contains, `/var/lib/snapd/desktop/applications/ r,`)
	// check generated rules against expected rules
	for _, rule := range opts.expectedRules {
		// early exit on error for less confusing debugigng
		c.Assert(apparmorSpec.SnippetForTag(securityTag), testutil.Contains, rule)
	}
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesHappy(c *C) {
	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{"org.example.desktop", "org.example.Foo.desktop", "org.example.Bar.desktop", "bar.desktop"},
		desktopFileIDs: []string{"org.example", "org.example.Foo"},
		isInstance:     false,
		expectedRules: []string{
			// allow rules for snap's desktop files
			fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", dirs.SnapDesktopFilesDir),        // prefixed with DesktopPrefix()
			fmt.Sprintf("%s r,", filepath.Join(dirs.SnapDesktopFilesDir, "org.example.desktop")),     // desktop-file-ids, unchanged
			fmt.Sprintf("%s r,", filepath.Join(dirs.SnapDesktopFilesDir, "org.example.Foo.desktop")), // desktop-file-ids, unchanged
			// check all deny patterns are generated
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "[^so]**.desktop")), // ^s from some-snap and ^o from org
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{o[^r],s[^o]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{or[^g],so[^m]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org[^.],som[^e]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.[^e],some[^-]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.e[^x],some-[^s]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.ex[^a],some-s[^n]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.exa[^m],some-sn[^a]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.exam[^p],some-sna[^p]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.examp[^l],some-snap[^_]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.exampl[^e],some-snap_[^bo]}**.desktop")), // some-snap_ common prefix then diverging
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.example[^.],some-snap_b[^a],some-snap_o[^r]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.example.[^F],some-snap_ba[^r],some-snap_or[^g]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.example.F[^o],some-snap_org[^.]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "{org.example.Fo[^o],some-snap_org.[^e]}**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.e[^x]**.desktop")), // org.example.Foo.desktop ended
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.ex[^a]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.exa[^m]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.exam[^p]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.examp[^l]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.exampl[^e]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.example[^.]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.example.[^B]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.example.B[^a]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_org.example.Ba[^r]**.desktop")), // longest pattern
		},
	}
	s.testDesktopFileRules(c, opts)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesNoDesktopFilesFallback(c *C) {
	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{},
		desktopFileIDs: []string{"org.example"},
		isInstance:     false,
		expectedRules:  s.fallbackRules,
	}
	s.testDesktopFileRules(c, opts)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesSnapMountErrorFallback(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	restore = builtin.MockDesktopFilesFromInstalledSnap(func(s *snap.Info) ([]string, error) {
		return nil, errors.New("boom")
	})
	defer restore()

	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{"org.example.desktop", "org.example.Foo.desktop", "org.example.Bar.desktop", "bar.desktop"},
		desktopFileIDs: []string{"org.example"},
		isInstance:     false,
		expectedRules:  s.fallbackRules,
	}
	s.testDesktopFileRules(c, opts)

	c.Check(logbuf.String(), testutil.Contains, `failed to collect desktop files from snap "some-snap": boom`)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesAAREExclusionPatternsErrorFallback(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	restore = builtin.MockApparmorGenerateAAREExclusionPatterns(func(excludePatterns []string, opts *apparmorutils.AAREExclusionPatternsOptions) (string, error) {
		return "", errors.New("boom")
	})
	defer restore()

	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{"org.example.desktop", "org.example.Foo.desktop", "org.example.Bar.desktop", "bar.desktop"},
		desktopFileIDs: []string{"org.example"},
		isInstance:     false,
		expectedRules:  s.fallbackRules,
	}
	s.testDesktopFileRules(c, opts)

	c.Check(logbuf.String(), testutil.Contains, `internal error: failed to generate deny rules for snap "some-snap": boom`)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesCommonSnapNameAndDesktopFileID(c *C) {
	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{"some-snap.example.desktop", "foo.desktop"},
		desktopFileIDs: []string{"some-snap.example"},
		isInstance:     false,
		expectedRules: []string{
			// allow rules for snap's desktop files
			fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", dirs.SnapDesktopFilesDir),          // prefixed with DesktopPrefix()
			fmt.Sprintf("%s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap.example.desktop")), // desktop-file-ids, unchanged
			// check all deny patterns are generated
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "[^s]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "s[^o]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "so[^m]**.desktop")),
			// ... skip some patterns
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-sna[^p]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap[^_.]**.desktop")), // some-snap common prefix then diverging
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap{.[^e],_[^f]}**.desktop")),
		},
	}
	s.testDesktopFileRules(c, opts)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesSanitizedDesktopFileName(c *C) {
	opts := testDesktopFileRulesOptions{
		snapName:     "some-snap",
		desktopFiles: []string{`AaZz09. -,._?**[]{}^"\$#.desktop`},
		isInstance:   false,
		expectedRules: []string{
			// allow rules for snap's desktop files
			fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", dirs.SnapDesktopFilesDir),
			// check all deny patterns are generated
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "[^s]**.desktop")),
			// desktop file name was sanitized by snap.MangleDesktopFileName
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_AaZz09.[^_]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_AaZz09._-_._____[^_]**.desktop")),
			fmt.Sprintf("deny %s r,", filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_AaZz09._-_.____________[^_]**.desktop")),
		},
	}
	s.testDesktopFileRules(c, opts)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesBadDesktopFileName(c *C) {
	// Stress the case where a snap file name skipped sanitization somehow
	// This should never happen because snap.MangleDesktopFileName is called
	restore := builtin.MockDesktopFilesFromInstalledSnap(func(s *snap.Info) ([]string, error) {
		return []string{"foo**?$.desktop"}, nil
	})
	defer restore()

	opts := testDesktopFileRulesOptions{
		snapName:      "some-snap",
		desktopFiles:  []string{"foo**?$.desktop"},
		isInstance:    false,
		expectedRules: s.fallbackRules,
		expectedErr:   `internal error: invalid desktop file name "foo**?$.desktop" found in snap "some-snap": "foo**?$.desktop" contains a reserved apparmor char from ` + "?*[]{}^\"\x00",
	}
	s.testDesktopFileRules(c, opts)
}

func (s *desktopFileRulesBaseSuite) TestDesktopFileRulesBadDesktopFileIDs(c *C) {
	// Stress the case where a desktop file ids attribute skipped validation during
	// installation somehow
	restore := builtin.MockDesktopFilesFromInstalledSnap(func(s *snap.Info) ([]string, error) {
		return []string{"org.*.example.desktop"}, nil
	})
	defer restore()

	opts := testDesktopFileRulesOptions{
		snapName:       "some-snap",
		desktopFiles:   []string{"org.*.example.desktop"},
		desktopFileIDs: []string{"org.*.example"},
		expectedRules:  s.fallbackRules,
		expectedErr:    `internal error: invalid desktop file ID "org.*.example" found in snap "some-snap": "org.*.example" contains a reserved apparmor char from ` + "?*[]{}^\"\x00",
	}
	s.testDesktopFileRules(c, opts)
}

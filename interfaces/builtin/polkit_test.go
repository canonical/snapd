// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"crypto"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type polkitInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo

	daemonPath1  string
	daemonPath2  string
	restorePaths func()
}

var _ = Suite(&polkitInterfaceSuite{
	iface: builtin.MustInterface("polkit"),
})

func (s *polkitInterfaceSuite) SetUpSuite(c *C) {
	d := c.MkDir()
	s.daemonPath1 = filepath.Join(d, "polkitd-1")
	s.daemonPath2 = filepath.Join(d, "polkitd-2")
	s.restorePaths = builtin.MockPolkitDaemonPaths(s.daemonPath1, s.daemonPath2)
}

func (s *polkitInterfaceSuite) TearDownSuite(c *C) {
	s.restorePaths()
}

func (s *polkitInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})

	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 polkit:
  interface: polkit
`

	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "polkit")

	c.Assert(os.WriteFile(s.daemonPath1, nil, 0o600), IsNil)
	c.Assert(os.WriteFile(s.daemonPath2, nil, 0o600), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapPolkitPolicyDir, 0o700), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapPolkitRuleDir, 0o700), IsNil)
}

func mockPolkitPolicyConnectedPlug(c *C) (plug *interfaces.ConnectedPlug, plugInfo *snap.PlugInfo) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 polkit:
  action-prefix: org.example.foo
apps:
 app:
  command: foo
  plugs: [polkit]
`
	return MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")
}

func mockPolkitRuleConnectedPlug(c *C, hash string) (plug *interfaces.ConnectedPlug, plugInfo *snap.PlugInfo) {
	const mockPlugSnapInfoYamlTemplate = `name: other
version: 1.0
plugs:
 polkit:
   install-rules:
     - name: polkit.foo.rules
       sha3-384: %s
apps:
 app:
  command: foo
  plugs: [polkit]
`
	mockPlugSnapInfoYaml := fmt.Sprintf(mockPlugSnapInfoYamlTemplate, hash)
	return MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")
}

func (s *polkitInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "polkit")
}

func (s *polkitInterfaceSuite) TestConnectedPlugAppArmorPolkitPolicy(c *C) {
	plug, _ := mockPolkitPolicyConnectedPlug(c)

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "# Description: Can talk to polkitd's CheckAuthorization API")
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `member="{,Cancel}CheckAuthorization"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugAppArmorPolkitRule(c *C) {
	plug, _ := mockPolkitRuleConnectedPlug(c, "hash")

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	// Apparmor rules are only injected when "action-prefix" is set.
	c.Assert(apparmorSpec.SecurityTags(), IsNil)
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Not(testutil.Contains), "# Description: Can talk to polkitd's CheckAuthorization API")
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Not(testutil.Contains), `member="{,Cancel}CheckAuthorization"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicy(c *C) {
	plug, plugInfo := mockPolkitPolicyConnectedPlug(c)

	const samplePolicy1 = `<policyconfig>
  <action id="org.example.foo.some-action">
    <description>Some action</description>
    <message>Authentication is required to do some action</message>
    <defaults>
      <allow_any>no</allow_any>
      <allow_inactive>no</allow_inactive>
      <allow_active>auth_admin</allow_active>
    </defaults>
  </action>
</policyconfig>`
	const samplePolicy2 = `<policyconfig/>`

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	policyPath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy1), 0o644), IsNil)
	policyPath = filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.bar.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy2), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)

	c.Check(polkitSpec.Policies(), DeepEquals, map[string]polkit.Policy{
		"polkit.foo": polkit.Policy(samplePolicy1),
		"polkit.bar": polkit.Policy(samplePolicy2),
	})
}

func computeRuleHash(c *C, data []byte) string {
	h := crypto.SHA3_384.New()
	_, err := h.Write(data)
	c.Assert(err, IsNil)
	digest := h.Sum(nil)
	hash, err := asserts.EncodeDigest(crypto.SHA3_384, digest)
	c.Assert(err, IsNil)
	return hash
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRule(c *C) {
	sampleRule1 := []byte("// js code - 1")
	hash1 := computeRuleHash(c, sampleRule1)
	sampleRule2 := []byte("// js code - 2")
	hash2 := computeRuleHash(c, sampleRule2)

	const mockPlugSnapInfoYamlTemplate = `name: other
version: 1.0
plugs:
 polkit:
   install-rules:
     - name: polkit.foo.rules
       sha3-384: %s
     - name: polkit.bar.rules
       sha3-384: %s
apps:
 app:
  command: foo
  plugs: [polkit]
`
	mockPlugSnapInfoYaml := fmt.Sprintf(mockPlugSnapInfoYamlTemplate, hash1, hash2)
	plug, plugInfo := MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.rules")
	c.Assert(os.WriteFile(rulePath, sampleRule1, 0o644), IsNil)
	rulePath = filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.bar.rules")
	c.Assert(os.WriteFile(rulePath, sampleRule2, 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)

	c.Check(polkitSpec.Rules(), DeepEquals, map[string]polkit.Rule{
		"polkit.foo": polkit.Rule(sampleRule1),
		"polkit.bar": polkit.Rule(sampleRule2),
	})
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyMissing(c *C) {
	plug, _ := mockPolkitPolicyConnectedPlug(c)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot find any policy files for plug "polkit"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRuleMissing(c *C) {
	plug, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot find any rule files for plug "polkit"`)

	// Files without plug-name prefix are not read.
	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/foo.rules")
	c.Assert(os.WriteFile(rulePath, []byte("// js code"), 0o644), IsNil)

	err = polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot find any rule files for plug "polkit"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyNotFile(c *C) {
	plug, plugInfo := mockPolkitPolicyConnectedPlug(c)

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	policyPath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.Mkdir(policyPath, 0o755), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot read file ".*/meta/polkit/polkit.foo.policy": read .*: is a directory`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRuleNotFile(c *C) {
	plug, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.rules")
	c.Assert(os.Mkdir(rulePath, 0o755), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot obtain hash of ".*/meta/polkit/polkit.foo.rules": read .*: is a directory`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyBadXML(c *C) {
	plug, plugInfo := mockPolkitPolicyConnectedPlug(c)

	const samplePolicy = `<malformed`
	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	policyPath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `cannot validate policy file ".*/meta/polkit/polkit.foo.policy": XML syntax error on line 1: unexpected EOF`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyBadAction(c *C) {
	plug, plugInfo := mockPolkitPolicyConnectedPlug(c)

	const samplePolicy = `<policyconfig>
  <action id="org.freedesktop.systemd1.manage-units">
    <description>A conflict with systemd's polkit actions</description>
    <message>Manage system services</message>
    <defaults>
      <allow_any>yes</allow_any>
    </defaults>
  </action>
</policyconfig>`
	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	policyPath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `policy file ".*/meta/polkit/polkit.foo.policy" contains unexpected action ID "org.freedesktop.systemd1.manage-units"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyBadImplies(c *C) {
	plug, plugInfo := mockPolkitPolicyConnectedPlug(c)

	const samplePolicy = `<policyconfig>
  <action id="org.example.foo.some-action">
    <description>Some action</description>
    <message>Allow "some action" (and also managing system services for some reason)</message>
    <defaults>
      <allow_any>yes</allow_any>
    </defaults>
    <annotate key="org.freedesktop.policykit.imply">org.freedesktop.systemd1.manage-units</annotate>
  </action>
</policyconfig>`
	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	policyPath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `policy file ".*/meta/polkit/polkit.foo.policy" contains unexpected action ID "org.freedesktop.systemd1.manage-units"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRuleNoMatchingEntry(c *C) {
	plug, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.no-match.rules")
	c.Assert(os.WriteFile(rulePath, []byte("// js code"), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `no matching "install-rule" entry found for ".*/meta/polkit/polkit.no-match.rules"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRuleBadHash(c *C) {
	plug, plugInfo := mockPolkitRuleConnectedPlug(c, "bad-hash")

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.rules")
	c.Assert(os.WriteFile(rulePath, []byte("// js code - 1"), 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, ErrorMatches, `unexpected hash digest of ".*/meta/polkit/polkit.foo.rules", expected "bad-hash", found ".*"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitRuleBadFileSize(c *C) {
	plug, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	c.Assert(os.MkdirAll(filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit"), 0o755), IsNil)
	rulePath := filepath.Join(plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.rules")
	ruleContent := make([]byte, 128*1024+1)
	n, err := rand.Read(ruleContent)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 128*1024+1)
	c.Assert(os.WriteFile(rulePath, ruleContent, 0o644), IsNil)

	polkitSpec := &polkit.Specification{}
	err = polkitSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, ErrorMatches, `".*/meta/polkit/polkit.foo.rules" is 131073 bytes, max rule file size is 131072`)
}

func (s *polkitInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugPolicy(c *C) {
	_, plugInfo := mockPolkitPolicyConnectedPlug(c)

	c.Assert(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugRule(c *C) {
	_, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	c.Assert(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugPolicyHappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
  action-prefix: org.example.bar
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["polkit"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugRuleHappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
   install-rules:
     - name: foo.bar.rules
       sha3-384: hash
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["polkit"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugPolicyDirNotWritable(c *C) {
	_, plugInfo := mockPolkitPolicyConnectedPlug(c)

	// Actions directory is not writable.
	c.Assert(os.Chmod(dirs.SnapPolkitPolicyDir, 0o500), IsNil)
	err := interfaces.BeforePreparePlug(s.iface, plugInfo)
	c.Assert(err, ErrorMatches, `cannot use "action-prefix" attribute: ".*/usr/share/polkit-1/actions" is not writable`)
}

func (s *polkitInterfaceSuite) TestSanitizePlugRuleDirNotWritable(c *C) {
	_, plugInfo := mockPolkitRuleConnectedPlug(c, "hash")

	// Rules directory is not writable.
	c.Assert(os.Chmod(dirs.SnapPolkitRuleDir, 0o500), IsNil)
	err := interfaces.BeforePreparePlug(s.iface, plugInfo)
	c.Assert(err, ErrorMatches, `cannot use "install-rules" attribute: ".*/etc/polkit-1/rules.d" is not writable`)
}

func (s *polkitInterfaceSuite) TestSanitizePlugPolicyUnhappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
  $t
`
	var testCases = []struct {
		inp    string
		errStr string
	}{
		{"", `snap "polkit-plug-snap" must have at least one of \("action-prefix", "install-rules"\) attributes set for interface "polkit"`},

		{`action-prefix: true`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type bool for "action-prefix" attribute: \*string`},
		{`action-prefix: 42`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type int64 for "action-prefix" attribute: \*string`},
		{`action-prefix: []`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type \[\]interface {} for "action-prefix" attribute: \*string`},
		{`action-prefix: {}`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type map\[string\]interface {} for "action-prefix" attribute: \*string`},

		{`action-prefix: ""`, `plug has invalid action-prefix: ""`},
		{`action-prefix: "org"`, `plug has invalid action-prefix: "org"`},
		{`action-prefix: "a+b"`, `plug has invalid action-prefix: "a\+b"`},
		{`action-prefix: "org.example\n"`, `plug has invalid action-prefix: "org.example\\n"`},
		{`action-prefix: "com.example "`, `plug has invalid action-prefix: "com.example "`},
		{`action-prefix: "123.foo.bar"`, `plug has invalid action-prefix: "123.foo.bar"`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["polkit"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *polkitInterfaceSuite) TestSanitizePlugRuleUnhappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
  $t
`
	mockRuleEntry := func(name, hash string) string {
		const ruleEntryTemplate = `install-rules:
   - name: %s
     sha3-384: %s`
		return fmt.Sprintf(ruleEntryTemplate, name, hash)
	}
	mockBadKey := `install-rules:
   - bad: value`
	mockMissingName := `install-rules:
   - sha3-384: hash`
	mockMissingHash := `install-rules:
   - name: test.rules`

	var testCases = []struct {
		inp    string
		errStr string
	}{
		{"", `snap "polkit-plug-snap" must have at least one of \("action-prefix", "install-rules"\) attributes set for interface "polkit"`},

		{`install-rules: true`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type bool for "install-rules" attribute: \*\[\]map\[string\]string`},
		{`install-rules: 42`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type int64 for "install-rules" attribute: \*\[\]map\[string\]string`},
		{`install-rules: {}`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type map\[string\]interface {} for "install-rules" attribute: \*\[\]map\[string\]string`},

		{`install-rules: []`, `"install-rules" must have at least one entry`},
		{mockRuleEntry(".rules", "hash"), `invalid polkit rule file name: rule file name cannot be empty`},
		{mockRuleEntry("test.policy", "hash"), `invalid polkit rule file name: "test.policy" must end with "\.rules"`},
		{mockBadKey, `unexpected key "bad" for "install-rules" entry`},
		{mockMissingName, `key "name" is required for "install-rules" entry`},
		{mockMissingHash, `key "sha3-384" is required for "install-rules" entry`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["polkit"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitPolicyInternalError(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 polkit:
  action-prefix: false
apps:
 app:
  command: foo
  plugs: [polkit]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 polkit:
  interface: polkit
`
	slot, _ := MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "polkit")
	plug, _ := MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, ErrorMatches, `snap "other" has interface "polkit" with invalid value type bool for "action-prefix" attribute: \*string`)
}

func (s *polkitInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	// ImplicitOnCore is only tested in TestPolkitPoliciesSupported and TestPolkitRulesSupported.
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows installing polkit rules and/or access to polkitd to check authorisation")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "polkit")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "polkit")
}

func (s *polkitInterfaceSuite) TestPolkitPoliciesSupported(c *C) {
	// From now the actions directory is writable so daemon permissions matter.
	c.Assert(os.Chmod(dirs.SnapPolkitPolicyDir, 0o700), IsNil)
	// But not the rules to isolate te StaticInfo checks.
	c.Assert(os.Chmod(dirs.SnapPolkitRuleDir, 0o500), IsNil)

	// Neither daemon is executable so polkit policies are not supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o600), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o600), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, false)

	// The 1st daemon is executable so polkit policies are supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o700), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o600), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)

	// The 2nd daemon is executable so polkit policies are supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o600), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o700), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)

	// From now own, both daemons are executable so mounts matter.
	c.Assert(os.Chmod(s.daemonPath1, 0o700), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o700), IsNil)

	// Actions directory is not writable so polkit policies are not supported.
	c.Assert(os.Chmod(dirs.SnapPolkitPolicyDir, 0o500), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, false)

	// Actions directory is writable so polkit policies are supported.
	c.Assert(os.Chmod(dirs.SnapPolkitPolicyDir, 0o700), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)
}

func (s *polkitInterfaceSuite) TestPolkitRulesSupported(c *C) {
	// From now the rules directory is writable so daemon permissions matter.
	c.Assert(os.Chmod(dirs.SnapPolkitRuleDir, 0o700), IsNil)
	// But not the actions directory to isolate te StaticInfo checks.
	c.Assert(os.Chmod(dirs.SnapPolkitPolicyDir, 0o500), IsNil)

	// Neither daemon is executable so polkit rules are not supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o600), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o600), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, false)

	// The 1st daemon is executable so polkit rules are supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o700), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o600), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)

	// The 2nd daemon is executable so polkit rules are supported.
	c.Assert(os.Chmod(s.daemonPath1, 0o600), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o700), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)

	// From now own, both daemons are executable so mounts matter.
	c.Assert(os.Chmod(s.daemonPath1, 0o700), IsNil)
	c.Assert(os.Chmod(s.daemonPath2, 0o700), IsNil)

	// Rules directory is not writable so polkit rules are not supported.
	c.Assert(os.Chmod(dirs.SnapPolkitRuleDir, 0o500), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, false)

	// Rules directory is writable so polkit rules are supported.
	c.Assert(os.Chmod(dirs.SnapPolkitRuleDir, 0o700), IsNil)
	c.Check(interfaces.StaticInfoOf(s.iface).ImplicitOnCore, Equals, true)
}

func (s *polkitInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

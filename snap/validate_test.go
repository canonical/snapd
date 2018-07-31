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

package snap_test

import (
	"fmt"
	"regexp"
	"strconv"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/snap"

	"github.com/snapcore/snapd/testutil"
)

type ValidateSuite struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateSuite{})

func createSampleApp() *AppInfo {
	socket := &SocketInfo{
		Name:         "sock",
		ListenStream: "$SNAP_COMMON/socket",
	}
	app := &AppInfo{
		Snap: &Info{
			SideInfo: SideInfo{
				RealName: "mysnap",
				Revision: R(20),
			},
		},
		Name:  "foo",
		Plugs: map[string]*PlugInfo{"network-bind": {}},
		Sockets: map[string]*SocketInfo{
			"sock": socket,
		},
	}
	socket.App = app
	return app
}

func (s *ValidateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(MockSanitizePlugsSlots(func(snapInfo *Info) {}))
}

func (s *ValidateSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *ValidateSuite) TestValidateName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
		// a regexp stresser
		"u-94903713687486543234157734673284536758",
	}
	for _, name := range validNames {
		err := ValidateName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// names cannot be too long
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"1111111111111111111111111111111111111111x",
		"x1111111111111111111111111111111111111111",
		"x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x",
		// a regexp stresser
		"u-9490371368748654323415773467328453675-",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	}
	for _, name := range invalidNames {
		err := ValidateName(name)
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateInstanceName(c *C) {
	validNames := []string{
		// plain names are also valid instance names
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		// snap instance
		"foo_bar",
		"foo_0123456789",
		"01game_0123456789",
		"foo_1", "foo_1234abcd",
	}
	for _, name := range validNames {
		err := ValidateInstanceName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// invalid names are also invalid instance names, just a few
		// samples
		"",
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"a--a",
		"a-",
		"a ", " a", "a a",
		"_",
		"ру́сский_язы́к",
	}
	for _, name := range invalidNames {
		err := ValidateInstanceName(name)
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
	invalidInstanceKeys := []string{
		// the snap names are valid, but instance keys are not
		"foo_", "foo_1-23", "foo_01234567890", "foo_123_456",
		"foo__bar",
	}
	for _, name := range invalidInstanceKeys {
		err := ValidateInstanceName(name)
		c.Assert(err, ErrorMatches, `invalid instance key: ".*"`)
	}

}

func (s *ValidateSuite) TestValidateVersion(c *C) {
	validVersions := []string{
		"0", "v1.0", "0.12+16.04.20160126-0ubuntu1",
		"1:6.0.1+r16-3", "1.0~", "1.0+", "README.~1~",
		"a+++++++++++++++++++++++++++++++",
		"AZaz:.+~-123",
	}
	for _, version := range validVersions {
		err := ValidateVersion(version)
		c.Assert(err, IsNil)
	}
	invalidVersionsTable := [][2]string{
		{"~foo", `must start with an ASCII alphanumeric (and not '~')`},
		{"+foo", `must start with an ASCII alphanumeric (and not '+')`},

		{"foo:", `must end with an ASCII alphanumeric or one of '+' or '~' (and not ':')`},
		{"foo.", `must end with an ASCII alphanumeric or one of '+' or '~' (and not '.')`},
		{"foo-", `must end with an ASCII alphanumeric or one of '+' or '~' (and not '-')`},

		{"horrible_underscores", `contains invalid characters: "_"`},
		{"foo($bar^baz$)meep", `contains invalid characters: "($", "^", "$)"`},

		{"árbol", `must be printable, non-whitespace ASCII`},
		{"日本語", `must be printable, non-whitespace ASCII`},
		{"한글", `must be printable, non-whitespace ASCII`},
		{"ру́сский язы́к", `must be printable, non-whitespace ASCII`},

		{"~foo$bar:", `must start with an ASCII alphanumeric (and not '~'),` +
			` must end with an ASCII alphanumeric or one of '+' or '~' (and not ':'),` +
			` and contains invalid characters: "$"`},
	}
	for _, t := range invalidVersionsTable {
		version, reason := t[0], t[1]
		err := ValidateVersion(version)
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, fmt.Sprintf("invalid snap version %s: %s", strconv.QuoteToASCII(version), reason))
	}
	// version cannot be empty
	c.Assert(ValidateVersion(""), ErrorMatches, `invalid snap version: cannot be empty`)
	// version length cannot be >32
	c.Assert(ValidateVersion("this-version-is-a-little-bit-older"), ErrorMatches,
		`invalid snap version "this-version-is-a-little-bit-older": cannot be longer than 32 characters \(got: 34\)`)
}

func (s *ValidateSuite) TestValidateLicense(c *C) {
	validLicenses := []string{
		"GPL-3.0", "(GPL-3.0)", "GPL-3.0+", "GPL-3.0 AND GPL-2.0", "GPL-3.0 OR GPL-2.0", "MIT OR (GPL-3.0 AND GPL-2.0)", "MIT OR(GPL-3.0 AND GPL-2.0)",
	}
	for _, epoch := range validLicenses {
		err := ValidateLicense(epoch)
		c.Assert(err, IsNil)
	}
	invalidLicenses := []string{
		"GPL~3.0", "3.0-GPL", "(GPL-3.0", "(GPL-3.0))", "GPL-3.0++", "+GPL-3.0", "GPL-3.0 GPL-2.0",
	}
	for _, epoch := range invalidLicenses {
		err := ValidateLicense(epoch)
		c.Assert(err, NotNil)
	}
}

func (s *ValidateSuite) TestValidateHook(c *C) {
	validHooks := []*HookInfo{
		{Name: "a"},
		{Name: "aaa"},
		{Name: "a-a"},
		{Name: "aa-a"},
		{Name: "a-aa"},
		{Name: "a-b-c"},
	}
	for _, hook := range validHooks {
		err := ValidateHook(hook)
		c.Assert(err, IsNil)
	}
	invalidHooks := []*HookInfo{
		{Name: ""},
		{Name: "a a"},
		{Name: "a--a"},
		{Name: "-a"},
		{Name: "a-"},
		{Name: "0"},
		{Name: "123"},
		{Name: "123abc"},
		{Name: "日本語"},
	}
	for _, hook := range invalidHooks {
		err := ValidateHook(hook)
		c.Assert(err, ErrorMatches, `invalid hook name: ".*"`)
	}
}

// ValidateApp

func (s *ValidateSuite) TestValidateAppName(c *C) {
	validAppNames := []string{
		"1", "a", "aa", "aaa", "aaaa", "Aa", "aA", "1a", "a1", "1-a", "a-1",
		"a-a", "aa-a", "a-aa", "a-b-c", "0a-a", "a-0a",
	}
	for _, name := range validAppNames {
		c.Check(ValidateApp(&AppInfo{Name: name}), IsNil)
	}
	invalidAppNames := []string{
		"", "-", "--", "a--a", "a-", "a ", " a", "a a", "日本語", "한글",
		"ру́сский язы́к", "ໄຂ່​ອີ​ສ​ເຕີ້", ":a", "a:", "a:a", "_a", "a_", "a_a",
	}
	for _, name := range invalidAppNames {
		err := ValidateApp(&AppInfo{Name: name})
		c.Assert(err, ErrorMatches, `cannot have ".*" as app name.*`)
	}
}

func (s *ValidateSuite) TestValidateAppSockets(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].SocketMode = 0600
	c.Check(ValidateApp(app), IsNil)
}

func (s *ValidateSuite) TestValidateAppSocketsEmptyPermsOk(c *C) {
	app := createSampleApp()
	c.Check(ValidateApp(app), IsNil)
}

func (s *ValidateSuite) TestValidateAppSocketsWrongPerms(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].SocketMode = 1234
	err := ValidateApp(app)
	c.Assert(err, ErrorMatches, `cannot use socket mode: 2322`)
}

func (s *ValidateSuite) TestValidateAppSocketsMissingNetworkBindPlug(c *C) {
	app := createSampleApp()
	delete(app.Plugs, "network-bind")
	err := ValidateApp(app)
	c.Assert(
		err, ErrorMatches,
		`"network-bind" interface plug is required when sockets are used`)
}

func (s *ValidateSuite) TestValidateAppSocketsEmptyListenStream(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].ListenStream = ""
	err := ValidateApp(app)
	c.Assert(err, ErrorMatches, `socket "sock" must define "listen-stream"`)
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidName(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].Name = "invalid name"
	err := ValidateApp(app)
	c.Assert(err, ErrorMatches, `invalid socket name: "invalid name"`)
}

func (s *ValidateSuite) TestValidateAppSocketsValidListenStreamAddresses(c *C) {
	app := createSampleApp()
	validListenAddresses := []string{
		// socket paths using variables as prefix
		"$SNAP_DATA/my.socket",
		"$SNAP_COMMON/my.socket",
		// abstract sockets
		"@snap.mysnap.my.socket",
		// addresses and ports
		"1",
		"1023",
		"1024",
		"65535",
		"127.0.0.1:8080",
		"[::]:8080",
		"[::1]:8080",
	}
	socket := app.Sockets["sock"]
	for _, validAddress := range validListenAddresses {
		socket.ListenStream = validAddress
		err := ValidateApp(app)
		c.Check(err, IsNil, Commentf(validAddress))
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamPath(c *C) {
	app := createSampleApp()
	invalidListenAddresses := []string{
		// socket paths out of the snap dirs
		"/some/path/my.socket",
		"/var/snap/mysnap/20/my.socket", // path is correct but has hardcoded prefix
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Assert(err, ErrorMatches, `socket "sock" has invalid "listen-stream": only.*are allowed`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamPathContainsDots(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].ListenStream = "$SNAP/../some.path"
	err := ValidateApp(app)
	c.Assert(
		err, ErrorMatches,
		`socket "sock" has invalid "listen-stream": "\$SNAP/../some.path" should be written as "some.path"`)
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamPathPrefix(c *C) {
	app := createSampleApp()
	invalidListenAddresses := []string{
		"$SNAP/my.socket", // snap dir is not writable
		"$SOMEVAR/my.socket",
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Assert(
			err, ErrorMatches,
			`socket "sock" has invalid "listen-stream": only \$SNAP_DATA and \$SNAP_COMMON prefixes are allowed`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamAbstractSocket(c *C) {
	app := createSampleApp()
	invalidListenAddresses := []string{
		"@snap.mysnap",
		"@snap.mysnap\000.foo",
		"@snap.notmysnap.my.socket",
		"@some.other.name",
		"@snap.myappiswrong.foo",
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Assert(err, ErrorMatches, `socket "sock" path for "listen-stream" must be prefixed with.*`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamAddress(c *C) {
	app := createSampleApp()
	invalidListenAddresses := []string{
		"10.0.1.1:8080",
		"[fafa::baba]:8080",
		"127.0.0.1\000:8080",
		"127.0.0.1::8080",
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Assert(err, ErrorMatches, `socket "sock" has invalid "listen-stream" address.*`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamPort(c *C) {
	app := createSampleApp()
	invalidPorts := []string{
		"0",
		"66536",
		"-8080",
		"12312345345",
		"[::]:-123",
		"[::1]:3452345234",
		"invalid",
		"[::]:invalid",
	}
	socket := app.Sockets["sock"]
	for _, invalidPort := range invalidPorts {
		socket.ListenStream = invalidPort
		err := ValidateApp(app)
		c.Assert(err, ErrorMatches, `socket "sock" has invalid "listen-stream" port number.*`)
	}
}

func (s *ValidateSuite) TestAppWhitelistSimple(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo"}), IsNil)
}

func (s *ValidateSuite) TestAppWhitelistWithVars(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo $SNAP_DATA"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo $SNAP_DATA"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo $SNAP_DATA"}), IsNil)
}

func (s *ValidateSuite) TestAppWhitelistIllegal(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "x\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "test!me"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", BusName: "foo\n"}), NotNil)
}

func (s *ValidateSuite) TestAppDaemonValue(c *C) {
	for _, t := range []struct {
		daemon string
		ok     bool
	}{
		// good
		{"", true},
		{"simple", true},
		{"forking", true},
		{"oneshot", true},
		{"dbus", true},
		{"notify", true},
		// bad
		{"invalid-thing", false},
	} {
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon}), ErrorMatches, fmt.Sprintf(`"daemon" field contains invalid value %q`, t.daemon))
		}
	}
}

func (s *ValidateSuite) TestAppStopMode(c *C) {
	// check services
	for _, t := range []struct {
		stopMode StopModeType
		ok       bool
	}{
		// good
		{"", true},
		{"sigterm", true},
		{"sigterm-all", true},
		{"sighup", true},
		{"sighup-all", true},
		{"sigusr1", true},
		{"sigusr1-all", true},
		{"sigusr2", true},
		{"sigusr2-all", true},
		// bad
		{"invalid-thing", false},
	} {
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", StopMode: t.stopMode}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", StopMode: t.stopMode}), ErrorMatches, fmt.Sprintf(`"stop-mode" field contains invalid value %q`, t.stopMode))
		}
	}

	// non-services cannot have a stop-mode
	err := ValidateApp(&AppInfo{Name: "foo", Daemon: "", StopMode: "sigterm"})
	c.Check(err, ErrorMatches, `"stop-mode" cannot be used for "foo", only for services`)
}

func (s *ValidateSuite) TestAppRefreshMode(c *C) {
	// check services
	for _, t := range []struct {
		refreshMode string
		ok          bool
	}{
		// good
		{"", true},
		{"endure", true},
		{"restart", true},
		// bad
		{"invalid-thing", false},
	} {
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", RefreshMode: t.refreshMode}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", RefreshMode: t.refreshMode}), ErrorMatches, fmt.Sprintf(`"refresh-mode" field contains invalid value %q`, t.refreshMode))
		}
	}

	// non-services cannot have a refresh-mode
	err := ValidateApp(&AppInfo{Name: "foo", Daemon: "", RefreshMode: "endure"})
	c.Check(err, ErrorMatches, `"refresh-mode" cannot be used for "foo", only for services`)
}

func (s *ValidateSuite) TestAppWhitelistError(c *C) {
	err := ValidateApp(&AppInfo{Name: "foo", Command: "x\n"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `app description field 'command' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:$-]*$')`)
}

// Validate

func (s *ValidateSuite) TestDetectIllegalYamlBinaries(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: someething
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, NotNil)
}

func (s *ValidateSuite) TestDetectIllegalYamlService(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: something
   daemon: forking
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, NotNil)
}

func (s *ValidateSuite) TestIllegalSnapName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo.something
version: 1.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid snap name: "foo.something"`)
}

func (s *ValidateSuite) TestValidateChecksName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`
version: 1.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `snap name cannot be empty`)
}

func (s *ValidateSuite) TestIllegalSnapEpoch(c *C) {
	_, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
epoch: 0*
`))
	c.Assert(err, ErrorMatches, `.*invalid epoch.*`)
}

func (s *ValidateSuite) TestMissingSnapEpochIsOkay(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
`))
	c.Assert(err, IsNil)
	c.Assert(Validate(info), IsNil)
}

func (s *ValidateSuite) TestIllegalSnapLicense(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
license: GPL~3.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot validate license "GPL~3.0": unknown license: GPL~3.0`)
}

func (s *ValidateSuite) TestMissingSnapLicenseIsOkay(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
`))
	c.Assert(err, IsNil)
	c.Assert(Validate(info), IsNil)
}

func (s *ValidateSuite) TestIllegalHookName(c *C) {
	hookType := NewHookType(regexp.MustCompile(".*"))
	restore := MockSupportedHookTypes([]*HookType{hookType})
	defer restore()

	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
hooks:
  123abc:
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid hook name: "123abc"`)
}

func (s *ValidateSuite) TestPlugSlotNamesUnique(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: snap
version: 0
plugs:
 foo:
slots:
 foo:
`))
	c.Assert(err, IsNil)
	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have plug and slot with the same name: "foo"`)
}

func (s *ValidateSuite) TestIllegalAliasName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
  foo:
    aliases: [foo$]
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have "foo\$" as alias name for app "foo" - use only letters, digits, dash, underscore and dot characters`)
}

func (s *ValidateSuite) TestValidateAlias(c *C) {
	validAliases := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"a_a", "aa_a", "a_aa", "a_b_c",
		"a0", "a_0", "a_0a",
		"01game", "1-or-2",
		"0.1game", "1_or_2",
	}
	for _, alias := range validAliases {
		err := ValidateAlias(alias)
		c.Assert(err, IsNil)
	}
	invalidAliases := []string{
		"",
		"_foo",
		"-foo",
		".foo",
		"foo$",
	}
	for _, alias := range invalidAliases {
		err := ValidateAlias(alias)
		c.Assert(err, ErrorMatches, `invalid alias name: ".*"`)
	}
}

type testConstraint string

func (constraint testConstraint) IsOffLimits(path string) bool {
	return true
}

func (s *ValidateSuite) TestValidateLayout(c *C) {
	si := &Info{SuggestedName: "foo"}
	// Several invalid layouts.
	c.Check(ValidateLayout(&Layout{Snap: si}, nil),
		ErrorMatches, "layout cannot use an empty path")
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Bind: "/bar", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Bind: "/bar", BindFile: "/froz"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Symlink: "/bar", BindFile: "/froz"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "tmpfs", BindFile: "/froz"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Bind: "/bar", Symlink: "/froz"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "tmpfs", Symlink: "/froz"}, nil),
		ErrorMatches, `layout "/foo" must define a bind mount, a filesystem mount or a symlink`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "ext4"}, nil),
		ErrorMatches, `layout "/foo" uses invalid filesystem "ext4"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo/bar", Type: "tmpfs", User: "foo"}, nil),
		ErrorMatches, `layout "/foo/bar" uses invalid user "foo"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo/bar", Type: "tmpfs", Group: "foo"}, nil),
		ErrorMatches, `layout "/foo/bar" uses invalid group "foo"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "tmpfs", Mode: 02755}, nil),
		ErrorMatches, `layout "/foo" uses invalid mode 02755`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$FOO", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "\$FOO" uses invalid mount point: reference to unknown variable "\$FOO"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Bind: "$BAR"}, nil),
		ErrorMatches, `layout "/foo" uses invalid bind mount source "\$BAR": reference to unknown variable "\$BAR"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Bind: "/etc"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid bind mount source "/etc": must start with \$SNAP, \$SNAP_DATA or \$SNAP_COMMON`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Symlink: "$BAR"}, nil),
		ErrorMatches, `layout "/foo" uses invalid symlink old name "\$BAR": reference to unknown variable "\$BAR"`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Symlink: "/etc"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid symlink old name "/etc": must start with \$SNAP, \$SNAP_DATA or \$SNAP_COMMON`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo/bar", Bind: "$SNAP/bar/foo"}, []LayoutConstraint{testConstraint("/foo")}),
		ErrorMatches, `layout "/foo/bar" underneath prior layout item "/foo"`)

	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/dev", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/dev" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/dev/foo", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/dev/foo" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/proc", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/proc" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/sys", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/sys" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/run", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/run" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/boot", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/boot" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/lost+found", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/lost\+found" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/media", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/media" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/snap", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/snap" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/lib/snapd", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/lib/snapd" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/lib/snapd/hostfs", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/lib/snapd/hostfs" in an off-limits area`)

	// Several valid layouts.
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "tmpfs", Mode: 01755}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/tmp", Type: "tmpfs"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/usr", Bind: "$SNAP/usr"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var", Bind: "$SNAP_DATA/var"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var", Bind: "$SNAP_COMMON/var"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/etc/foo.conf", Symlink: "$SNAP_DATA/etc/foo.conf"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/a/b", Type: "tmpfs", User: "root"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/a/b", Type: "tmpfs", Group: "root"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/a/b", Type: "tmpfs", Mode: 0655}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/usr", Symlink: "$SNAP/usr"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var", Symlink: "$SNAP_DATA/var"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var", Symlink: "$SNAP_COMMON/var"}, nil), IsNil)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/data", Symlink: "$SNAP_DATA"}, nil), IsNil)
}

func (s *ValidateSuite) TestValidateLayoutAll(c *C) {
	// /usr/foo prevents /usr/foo/bar from being valid (tmpfs)
	const yaml1 = `
name: broken-layout-1
layout:
  /usr/foo:
    type: tmpfs
  /usr/foo/bar:
    type: tmpfs
`
	const yaml1rev = `
name: broken-layout-1
layout:
  /usr/foo/bar:
    type: tmpfs
  /usr/foo:
    type: tmpfs
`

	for _, yaml := range []string{yaml1, yaml1rev} {
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)})
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 2)
		err = ValidateLayoutAll(info)
		c.Assert(err, ErrorMatches, `layout "/usr/foo/bar" underneath prior layout item "/usr/foo"`)
	}

	// Same as above but with bind-mounts instead of filesystem mounts.
	const yaml2 = `
name: broken-layout-2
layout:
  /usr/foo:
    bind: $SNAP
  /usr/foo/bar:
    bind: $SNAP
`
	const yaml2rev = `
name: broken-layout-2
layout:
  /usr/foo/bar:
    bind: $SNAP
  /usr/foo:
    bind: $SNAP
`
	for _, yaml := range []string{yaml2, yaml2rev} {
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)})
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 2)
		err = ValidateLayoutAll(info)
		c.Assert(err, ErrorMatches, `layout "/usr/foo/bar" underneath prior layout item "/usr/foo"`)
	}

	// /etc/foo (directory) is not clashing with /etc/foo.conf (file)
	const yaml3 = `
name: valid-layout-1
layout:
  /etc/foo:
    bind: $SNAP_DATA/foo
  /etc/foo.conf:
    symlink: $SNAP_DATA/foo.conf
`
	const yaml3rev = `
name: valid-layout-1
layout:
  /etc/foo.conf:
    symlink: $SNAP_DATA/foo.conf
  /etc/foo:
    bind: $SNAP_DATA/foo
`
	for _, yaml := range []string{yaml3, yaml3rev} {
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)})
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 2)
		err = ValidateLayoutAll(info)
		c.Assert(err, IsNil)
	}

	// /etc/foo file is not clashing with /etc/foobar
	const yaml4 = `
name: valid-layout-2
layout:
  /etc/foo:
    symlink: $SNAP_DATA/foo
  /etc/foobar:
    symlink: $SNAP_DATA/foobar
`
	const yaml4rev = `
name: valid-layout-2
layout:
  /etc/foobar:
    symlink: $SNAP_DATA/foobar
  /etc/foo:
    symlink: $SNAP_DATA/foo
`
	for _, yaml := range []string{yaml4, yaml4rev} {
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)})
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 2)
		err = ValidateLayoutAll(info)
		c.Assert(err, IsNil)
	}

	// /etc/foo file is also clashing with /etc/foo/bar
	const yaml5 = `
name: valid-layout-2
layout:
  /usr/foo:
    symlink: $SNAP_DATA/foo
  /usr/foo/bar:
    bind: $SNAP_DATA/foo/bar
`
	const yaml5rev = `
name: valid-layout-2
layout:
  /usr/foo/bar:
    bind: $SNAP_DATA/foo/bar
  /usr/foo:
    symlink: $SNAP_DATA/foo
`
	for _, yaml := range []string{yaml5, yaml5rev} {
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)})
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 2)
		err = ValidateLayoutAll(info)
		c.Assert(err, ErrorMatches, `layout "/usr/foo/bar" underneath prior layout item "/usr/foo"`)
	}

	const yaml6 = `
name: tricky-layout-1
layout:
  /etc/norf:
    bind: $SNAP/etc/norf
  /etc/norf:
    bind-file: $SNAP/etc/norf
`
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml6), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 1)
	err = ValidateLayoutAll(info)
	c.Assert(err, IsNil)
	c.Assert(info.Layout["/etc/norf"].Bind, Equals, "")
	c.Assert(info.Layout["/etc/norf"].BindFile, Equals, "$SNAP/etc/norf")

	// Two layouts refer to the same path as a directory and a file.
	const yaml7 = `
name: clashing-source-path-1
layout:
  /etc/norf:
    bind: $SNAP/etc/norf
  /etc/corge:
    bind-file: $SNAP/etc/norf
`
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml7), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, ErrorMatches, `layout "/etc/norf" refers to directory "\$SNAP/etc/norf" but another layout treats it as file`)

	// Two layouts refer to the same path as a directory and a file (other way around).
	const yaml8 = `
name: clashing-source-path-2
layout:
  /etc/norf:
    bind-file: $SNAP/etc/norf
  /etc/corge:
    bind: $SNAP/etc/norf
`
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml8), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, ErrorMatches, `layout "/etc/norf" refers to file "\$SNAP/etc/norf" but another layout treats it as a directory`)

	// Two layouts refer to the same path, but one uses variable and the other doesn't.
	const yaml9 = `
name: clashing-source-path-3
layout:
  /etc/norf:
    bind-file: $SNAP/etc/norf
  /etc/corge:
    bind: /snap/clashing-source-path-3/42/etc/norf
`
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml9), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, ErrorMatches, `layout "/etc/norf" refers to file "\$SNAP/etc/norf" but another layout treats it as a directory`)

	// Same source path referred from a bind mount and symlink doesn't clash.
	const yaml10 = `
name: non-clashing-source-1
layout:
  /etc/norf:
    bind: $SNAP/etc/norf
  /etc/corge:
    symlink: $SNAP/etc/norf
`
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml10), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, IsNil)

	// Same source path referred from a file bind mount and symlink doesn't clash.
	const yaml11 = `
name: non-clashing-source-1
layout:
  /etc/norf:
    bind-file: $SNAP/etc/norf
  /etc/corge:
    symlink: $SNAP/etc/norf
`
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml11), &SideInfo{Revision: R(42)})
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, IsNil)
}

func (s *ValidateSuite) TestValidateSocketName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
	}
	for _, name := range validNames {
		err := ValidateSocketName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
		// no null chars in the string are allowed
		"aa-a\000-b",
	}
	for _, name := range invalidNames {
		err := ValidateSocketName(name)
		c.Assert(err, ErrorMatches, `invalid socket name: ".*"`)
	}
}

func (s *YamlSuite) TestValidateAppStartupOrder(c *C) {
	meta := []byte(`
name: foo
version: 1.0
`)
	fooAfterBaz := []byte(`
apps:
  foo:
    after: [baz]
    daemon: simple
  bar:
    daemon: forking
`)
	fooBeforeBaz := []byte(`
apps:
  foo:
    before: [baz]
    daemon: simple
  bar:
    daemon: forking
`)

	fooNotADaemon := []byte(`
apps:
  foo:
    after: [bar]
  bar:
    daemon: forking
`)

	fooBarNotADaemon := []byte(`
apps:
  foo:
    after: [bar]
    daemon: forking
  bar:
`)
	fooSelfCycle := []byte(`
apps:
  foo:
    after: [foo]
    daemon: forking
  bar:
`)
	// cycle between foo and bar
	badOrder1 := []byte(`
apps:
 foo:
   after: [bar]
   daemon: forking
 bar:
   after: [foo]
   daemon: forking
`)
	// conflicting schedule for baz
	badOrder2 := []byte(`
apps:
 foo:
   before: [bar]
   daemon: forking
 bar:
   after: [foo]
   daemon: forking
 baz:
   before: [foo]
   after: [bar]
   daemon: forking
`)
	// conflicting schedule for baz
	badOrder3Cycle := []byte(`
apps:
 foo:
   before: [bar]
   after: [zed]
   daemon: forking
 bar:
   before: [baz]
   daemon: forking
 baz:
   before: [zed]
   daemon: forking
 zed:
   daemon: forking
`)
	goodOrder1 := []byte(`
apps:
 foo:
   after: [bar, zed]
   daemon: oneshot
 bar:
   before: [foo]
   daemon: dbus
 baz:
   after: [foo]
   daemon: forking
 zed:
   daemon: dbus
`)
	goodOrder2 := []byte(`
apps:
 foo:
   after: [baz]
   daemon: oneshot
 bar:
   before: [baz]
   daemon: dbus
 baz:
   daemon: forking
 zed:
   daemon: dbus
   after: [foo, bar, baz]
`)

	tcs := []struct {
		name string
		desc []byte
		err  string
	}{{
		name: "foo after baz",
		desc: fooAfterBaz,
		err:  `application "foo" refers to missing application "baz" in before/after`,
	}, {
		name: "foo before baz",
		desc: fooBeforeBaz,
		err:  `application "foo" refers to missing application "baz" in before/after`,
	}, {
		name: "foo not a daemon",
		desc: fooNotADaemon,
		err:  `cannot define before/after in application "foo" as it's not a service`,
	}, {
		name: "foo wants bar, bar not a daemon",
		desc: fooBarNotADaemon,
		err:  `application "foo" refers to non-service application "bar" in before/after`,
	}, {
		name: "bad order 1",
		desc: badOrder1,
		err:  `applications are part of a before/after cycle: (foo, bar)|(bar, foo)`,
	}, {
		name: "bad order 2",
		desc: badOrder2,
		err:  `applications are part of a before/after cycle: ((foo|bar|baz)(, )?){3}`,
	}, {
		name: "bad order 3 - cycle",
		desc: badOrder3Cycle,
		err:  `applications are part of a before/after cycle: ((foo|bar|baz|zed)(, )?){4}`,
	}, {
		name: "all good, 3 apps",
		desc: goodOrder1,
	}, {
		name: "all good, 4 apps",
		desc: goodOrder2,
	}, {
		name: "self cycle",
		desc: fooSelfCycle,
		err:  `applications are part of a before/after cycle: foo`},
	}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (s *ValidateSuite) TestValidateAppWatchdog(c *C) {
	meta := []byte(`
name: foo
version: 1.0
`)
	fooAllGood := []byte(`
apps:
  foo:
    daemon: simple
    watchdog-timeout: 12s
`)
	fooNotADaemon := []byte(`
apps:
  foo:
    watchdog-timeout: 12s
`)

	fooNegative := []byte(`
apps:
  foo:
    daemon: simple
    watchdog-timeout: -12s
`)

	tcs := []struct {
		name string
		desc []byte
		err  string
	}{{
		name: "foo all good",
		desc: fooAllGood,
	}, {
		name: "foo not a service",
		desc: fooNotADaemon,
		err:  `cannot define watchdog-timeout in application "foo" as it's not a service`,
	}, {
		name: "negative timeout",
		desc: fooNegative,
		err:  `cannot use a negative watchdog-timeout in application "foo"`,
	}}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)
		c.Assert(info, NotNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (s *YamlSuite) TestValidateAppTimer(c *C) {
	meta := []byte(`
name: foo
version: 1.0
`)
	allGood := []byte(`
apps:
  foo:
    daemon: simple
    timer: 10:00-12:00
`)
	notAService := []byte(`
apps:
  foo:
    timer: 10:00-12:00
`)
	badTimer := []byte(`
apps:
  foo:
    daemon: oneshot
    timer: mon,10:00-12:00,mon2-wed3
`)

	tcs := []struct {
		name string
		desc []byte
		err  string
	}{{
		name: "all correct",
		desc: allGood,
	}, {
		name: "not a service",
		desc: notAService,
		err:  `cannot use timer with application "foo" as it's not a service`,
	}, {
		name: "invalid timer",
		desc: badTimer,
		err:  `application "foo" timer has invalid format: cannot parse "mon2-wed3": invalid schedule fragment`,
	}}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (s *ValidateSuite) TestValidateOsCannotHaveBase(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
type: os
base: bar
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have "base" field on "os" snap "foo"`)
}

func (s *ValidateSuite) TestValidateBaseCannotHaveBase(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
type: base
base: bar
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have "base" field on "base" snap "foo"`)
}

func (s *ValidateSuite) TestValidateCommonIDs(c *C) {
	meta := `
name: foo
version: 1.0
`
	good := meta + `
apps:
  foo:
    common-id: org.foo.foo
  bar:
    common-id: org.foo.bar
  baz:
`
	bad := meta + `
apps:
  foo:
    common-id: org.foo.foo
  bar:
    common-id: org.foo.foo
  baz:
`
	for i, tc := range []struct {
		meta string
		err  string
	}{
		{good, ""},
		{bad, `application ("bar" common-id "org.foo.foo" must be unique, already used by application "foo"|"foo" common-id "org.foo.foo" must be unique, already used by application "bar")`},
	} {
		c.Logf("tc #%v", i)
		info, err := InfoFromSnapYaml([]byte(tc.meta))
		c.Assert(err, IsNil)

		err = Validate(info)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}

func (s *validateSuite) TestValidatePlugSlotName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
	}
	for _, name := range validNames {
		c.Assert(ValidatePlugName(name), IsNil)
		c.Assert(ValidateSlotName(name), IsNil)
		c.Assert(ValidateInterfaceName(name), IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	}
	for _, name := range invalidNames {
		c.Assert(ValidatePlugName(name), ErrorMatches, `invalid plug name: ".*"`)
		c.Assert(ValidateSlotName(name), ErrorMatches, `invalid slot name: ".*"`)
		c.Assert(ValidateInterfaceName(name), ErrorMatches, `invalid interface name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateSnapInstanceNameBadSnapName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo_bad
version: 1.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid snap name: "foo_bad"`)
}

func (s *ValidateSuite) TestValidateSnapInstanceNameBadInstanceKey(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
`))
	c.Assert(err, IsNil)

	for _, s := range []string{"toolonginstance", "ABCD", "_", "inst@nce", "012345678901"} {
		info.InstanceKey = s
		err = Validate(info)
		c.Check(err, ErrorMatches, fmt.Sprintf("invalid instance key: %q", s))
	}
}

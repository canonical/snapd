// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2023 Canonical Ltd
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
	"sort"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/snapstate"
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
		Name:        "foo",
		Daemon:      "simple",
		DaemonScope: SystemDaemon,
		Plugs:       map[string]*PlugInfo{"network-bind": {}},
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
		{Name: "valid", CommandChain: []string{"valid"}},
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
	invalidHooks = []*HookInfo{
		{Name: "valid", CommandChain: []string{"in'valid"}},
		{Name: "valid", CommandChain: []string{"in valid"}},
	}
	for _, hook := range invalidHooks {
		err := ValidateHook(hook)
		c.Assert(err, ErrorMatches, `hook command-chain contains illegal.*`)
	}
}

// ValidateApp

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
	c.Assert(err, ErrorMatches, `invalid definition of socket "sock": cannot use mode: 2322`)
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
	c.Assert(err, ErrorMatches, `invalid definition of socket "sock": "listen-stream" is not defined`)
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidName(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].Name = "invalid name"
	err := ValidateApp(app)
	c.Assert(err, ErrorMatches, `invalid definition of socket "invalid name": invalid socket name: "invalid name"`)
}

func (s *ValidateSuite) TestValidateAppSocketsValidListenStreamAddresses(c *C) {
	app := createSampleApp()
	validListenAddresses := []string{
		// socket paths using variables as prefix
		"$SNAP_DATA/my.socket",
		"$SNAP_COMMON/my.socket",
		"$XDG_RUNTIME_DIR/my.socket",
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
		// Variables only valid for user mode services
		"$SNAP_USER_DATA/my.socket",
		"$SNAP_USER_COMMON/my.socket",
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Assert(err, ErrorMatches, `invalid definition of socket "sock": invalid "listen-stream": system daemon sockets must have a prefix of .*`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamPathContainsDots(c *C) {
	app := createSampleApp()
	app.Sockets["sock"].ListenStream = "$SNAP/../some.path"
	err := ValidateApp(app)
	c.Assert(
		err, ErrorMatches,
		`invalid definition of socket "sock": invalid "listen-stream": "\$SNAP/../some.path" should be written as "some.path"`)
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
			`invalid definition of socket "sock": invalid "listen-stream": system daemon sockets must have a prefix of \$SNAP_DATA, \$SNAP_COMMON or \$XDG_RUNTIME_DIR`)
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
		c.Assert(err, ErrorMatches, `invalid definition of socket "sock": path for "listen-stream" must be prefixed with.*`)
	}
}

func (s *ValidateSuite) TestValidateAppSocketsInvalidListenStreamAddress(c *C) {
	app := createSampleApp()
	app.Daemon = "simple"
	app.DaemonScope = SystemDaemon
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
		c.Assert(err, ErrorMatches, `invalid definition of socket "sock": invalid "listen-stream" address ".*", must be one of: 127\.0\.0\.1, \[::1\], \[::\]`)
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
		c.Assert(err, ErrorMatches, `invalid definition of socket "sock": invalid "listen-stream" port number.*`)
	}
}

func (s *ValidateSuite) TestValidateAppUserSocketsValidListenStreamAddresses(c *C) {
	app := createSampleApp()
	app.DaemonScope = UserDaemon
	validListenAddresses := []string{
		// socket paths using variables as prefix
		"$SNAP_USER_DATA/my.socket",
		"$SNAP_USER_COMMON/my.socket",
		"$XDG_RUNTIME_DIR/my.socket",
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

func (s *ValidateSuite) TestValidateAppUserSocketsInvalidListenStreamPath(c *C) {
	app := createSampleApp()
	app.DaemonScope = UserDaemon
	invalidListenAddresses := []string{
		// socket paths out of the snap dirs
		"/some/path/my.socket",
		// Variables only valid for system mode services
		"$SNAP_DATA/my.socket",
		"$SNAP_COMMON/my.socket",
	}
	socket := app.Sockets["sock"]
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Check(err, ErrorMatches, `invalid definition of socket "sock": invalid "listen-stream": user daemon sockets must have a prefix of .*`)
	}
}

func (s *ValidateSuite) TestValidateAppUserSocketsInvalidListenStreamAbstractSocket(c *C) {
	app := createSampleApp()
	app.DaemonScope = UserDaemon
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
		c.Assert(err, ErrorMatches, `invalid definition of socket "sock": path for "listen-stream" must be prefixed with.*`)
	}
}

func (s *ValidateSuite) TestValidateAppUserSocketsInvalidListenStreamPort(c *C) {
	app := createSampleApp()
	app.DaemonScope = UserDaemon
	invalidListenAddresses := []string{
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
	for _, invalidAddress := range invalidListenAddresses {
		socket.ListenStream = invalidAddress
		err := ValidateApp(app)
		c.Check(err, ErrorMatches, `invalid definition of socket "sock": invalid "listen-stream" port number .*`)
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
	c.Check(ValidateApp(&AppInfo{Name: "test'me"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", BusName: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", CommandChain: []string{"bar'baz"}}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", CommandChain: []string{"bar baz"}}), NotNil)
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
		var daemonScope DaemonScope
		if t.daemon != "" {
			daemonScope = SystemDaemon
		}
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon, DaemonScope: daemonScope}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon, DaemonScope: daemonScope}), ErrorMatches, fmt.Sprintf(`"daemon" field contains invalid value %q`, t.daemon))
		}
	}
}

func (s *ValidateSuite) TestAppDaemonScopeValue(c *C) {
	for _, t := range []struct {
		daemon      string
		daemonScope DaemonScope
		ok          bool
	}{
		// good
		{"", "", true},
		{"simple", SystemDaemon, true},
		{"simple", UserDaemon, true},
		// bad
		{"simple", "", false},
		{"", SystemDaemon, false},
		{"", UserDaemon, false},
		{"simple", "invalid-mode", false},
	} {
		app := &AppInfo{Name: "foo", Daemon: t.daemon, DaemonScope: t.daemonScope}
		err := ValidateApp(app)
		if t.ok {
			c.Check(err, IsNil)
		} else if t.daemon == "" {
			c.Check(err, ErrorMatches, `"daemon-scope" can only be set for daemons`)
		} else if t.daemonScope == "" {
			c.Check(err, ErrorMatches, `"daemon-scope" must be set for daemons`)
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`invalid "daemon-scope": %q`, t.daemonScope))
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
		{"sigint", true},
		{"sigint-all", true},
		// bad
		{"invalid-thing", false},
	} {
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", DaemonScope: SystemDaemon, StopMode: t.stopMode}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", DaemonScope: SystemDaemon, StopMode: t.stopMode}), ErrorMatches, fmt.Sprintf(`"stop-mode" field contains invalid value %q`, t.stopMode))
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
		daemon      string
		errMsg      string
	}{
		// good
		{"", "simple", ""},
		{"endure", "simple", ""},
		{"restart", "simple", ""},
		{"ignore-running", "", ""},
		// bad
		{"invalid-thing", "simple", `"refresh-mode" field contains invalid value "invalid-thing"`},
		{"endure", "", `"refresh-mode" for app "foo" can only have value "ignore-running"`},
		{"restart", "", `"refresh-mode" for app "foo" can only have value "ignore-running"`},
		{"ignore-running", "simple", `"refresh-mode" cannot be set to "ignore-running" for services`},
	} {
		var daemonScope DaemonScope
		if t.daemon != "" {
			daemonScope = SystemDaemon
		}

		err := ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon, DaemonScope: daemonScope, RefreshMode: t.refreshMode})
		if t.errMsg == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.errMsg)
		}
	}
}

func (s *ValidateSuite) TestAppWhitelistError(c *C) {
	err := ValidateApp(&AppInfo{Name: "foo", Command: "x\n"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `app description field 'command' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:$-]*$')`)
}

func (s *ValidateSuite) TestAppActivatesOn(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
slots:
  dbus-slot:
    interface: dbus
    bus: system
apps:
  server:
    daemon: simple
    activates-on: [dbus-slot]
`))
	c.Assert(err, IsNil)
	app := info.Apps["server"]
	c.Check(ValidateApp(app), IsNil)
}

func (s *ValidateSuite) TestAppActivatesOnNotDaemon(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
slots:
  dbus-slot:
apps:
  server:
    activates-on: [dbus-slot]
`))
	c.Assert(err, IsNil)
	app := info.Apps["server"]
	c.Check(ValidateApp(app), ErrorMatches, `activates-on is only applicable to services`)
}

func (s *ValidateSuite) TestAppActivatesOnSlotNotDbus(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
  server:
    daemon: simple
    slots: [network-bind]
    activates-on: [network-bind]
`))
	c.Assert(err, IsNil)
	app := info.Apps["server"]
	c.Check(ValidateApp(app), ErrorMatches, `invalid activates-on value "network-bind": slot does not use dbus interface`)
}

func (s *ValidateSuite) TestAppActivatesOnDaemonScopeMismatch(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
slots:
  dbus-slot:
    interface: dbus
    bus: session
apps:
  server:
    daemon: simple
    activates-on: [dbus-slot]
`))
	c.Assert(err, IsNil)
	app := info.Apps["server"]
	c.Check(ValidateApp(app), ErrorMatches, `invalid activates-on value "dbus-slot": bus "session" does not match daemon-scope "system"`)

	info, err = InfoFromSnapYaml([]byte(`name: foo
version: 1.0
slots:
  dbus-slot:
    interface: dbus
    bus: system
apps:
  server:
    daemon: simple
    daemon-scope: user
    activates-on: [dbus-slot]
`))
	c.Assert(err, IsNil)
	app = info.Apps["server"]
	c.Check(ValidateApp(app), ErrorMatches, `invalid activates-on value "dbus-slot": bus "system" does not match daemon-scope "user"`)
}

func (s *ValidateSuite) TestAppActivatesOnDuplicateApp(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
slots:
  dbus-slot:
    interface: dbus
    bus: system
apps:
  server:
    daemon: simple
    activates-on: [dbus-slot]
  dup:
    daemon: simple
    activates-on: [dbus-slot]
`))
	c.Assert(err, IsNil)
	app := info.Apps["server"]
	c.Check(ValidateApp(app), ErrorMatches, `invalid activates-on value "dbus-slot": slot is also activatable on app "dup"`)
}

// Validate

func (s *ValidateSuite) TestDetectInvalidProvenance(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
provenance: "--"
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid provenance: .*`)
}

func (s *ValidateSuite) TestDetectExplicitDefaultProvenance(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
provenance: global-upload
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `provenance cannot be set to default \(global-upload\) explicitly`)
}

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

func (s *ValidateSuite) TestIllegalHookDefaultConfigureWithoutConfigure(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
hooks:
  default-configure:
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, "cannot specify \"default-configure\" hook without \"configure\" hook")
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

func (s *ValidateSuite) TestValidatePlugSlotName(c *C) {
	const yaml1 = `
name: invalid-plugs
version: 1
plugs:
  p--lug: null
`
	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml1), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Plugs, HasLen, 1)
	err = Validate(info)
	c.Assert(err, ErrorMatches, `invalid plug name: "p--lug"`)

	const yaml2 = `
name: invalid-slots
version: 1
slots:
  s--lot: null
`
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml2), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Slots, HasLen, 1)
	err = Validate(info)
	c.Assert(err, ErrorMatches, `invalid slot name: "s--lot"`)

	const yaml3 = `
name: invalid-plugs-iface
version: 1
plugs:
  plug:
    interface: i--face
`
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml3), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Plugs, HasLen, 1)
	err = Validate(info)
	c.Assert(err, ErrorMatches, `invalid interface name "i--face" for plug "plug"`)

	const yaml4 = `
name: invalid-slots-iface
version: 1
slots:
  slot:
    interface: i--face
`
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml4), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Slots, HasLen, 1)
	err = Validate(info)
	c.Assert(err, ErrorMatches, `invalid interface name "i--face" for slot "slot"`)
}

func (s *ValidateSuite) TestValidateBaseNone(c *C) {
	const yaml = `name: requires-base
version: 1
base: none
`
	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)
	err = Validate(info)
	c.Assert(err, IsNil)
	c.Check(info.Base, Equals, "none")
}

func (s *ValidateSuite) TestValidateBaseNoneError(c *C) {
	yamlTemplate := `name: use-base-none
version: 1
base: none

%APPS_OR_HOOKS%
`
	const apps = `
apps:
  useradd:
    command: bin/true
`
	const hooks = `
hooks:
  configure:
`

	for _, appsOrHooks := range []string{apps, hooks} {
		yaml := strings.Replace(yamlTemplate, "%APPS_OR_HOOKS%", appsOrHooks, -1)
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
		c.Assert(err, IsNil)
		err = Validate(info)
		c.Assert(err, ErrorMatches, `cannot have apps or hooks with base "none"`)
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
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/home", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/home" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/proc", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/proc" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/sys", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/sys" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/run", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/run" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/run/foo", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/run/foo" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/run/systemd", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/run/systemd" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/run", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/run" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/run/foo", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/run/foo" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/var/run/systemd", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/var/run/systemd" in an off-limits area`)
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
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/lib/firmware", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/lib/firmware" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/lib/modules", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/lib/modules" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/usr/lib/firmware", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/usr/lib/firmware" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/usr/lib/modules", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/usr/lib/modules" in an off-limits area`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/tmp", Type: "tmpfs"}, nil),
		ErrorMatches, `layout "/tmp" in an off-limits area`)

	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Bind: "$SNAP/dev/sda[0123]"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid mount source: "/snap/foo/unset/dev/sda\[0123\]" contains a reserved apparmor char.*`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Bind: "$SNAP/*"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid mount source: "/snap/foo/unset/\*" contains a reserved apparmor char.*`)

	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", BindFile: "$SNAP/a\"quote"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid mount source: "/snap/foo/unset/a\\"quote" contains a reserved apparmor char.*`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", BindFile: "$SNAP/^invalid"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid mount source: "/snap/foo/unset/\^invalid" contains a reserved apparmor char.*`)

	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Symlink: "$SNAP/{here,there}"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid symlink: "/snap/foo/unset/{here,there}" contains a reserved apparmor char.*`)
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "$SNAP/evil", Symlink: "$SNAP/**"}, nil),
		ErrorMatches, `layout "\$SNAP/evil" uses invalid symlink: "/snap/foo/unset/\*\*" contains a reserved apparmor char.*`)

	// Several valid layouts.
	c.Check(ValidateLayout(&Layout{Snap: si, Path: "/foo", Type: "tmpfs", Mode: 01755}, nil), IsNil)
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
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)}, strk)
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
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)}, strk)
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
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)}, strk)
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
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)}, strk)
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
		strk := NewScopedTracker()
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), &SideInfo{Revision: R(42)}, strk)
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
	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml6), &SideInfo{Revision: R(42)}, strk)
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

	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml7), &SideInfo{Revision: R(42)}, strk)
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
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml8), &SideInfo{Revision: R(42)}, strk)
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
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml9), &SideInfo{Revision: R(42)}, strk)
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

	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml10), &SideInfo{Revision: R(42)}, strk)
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

	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml11), &SideInfo{Revision: R(42)}, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 2)
	err = ValidateLayoutAll(info)
	c.Assert(err, IsNil)

	// Layout replacing files in another snap's mount point
	const yaml12 = `
name: this-snap
layout:
  /snap/that-snap/current/stuff:
    symlink: $SNAP/stuff
`

	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml12), &SideInfo{Revision: R(42)}, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 1)
	err = ValidateLayoutAll(info)
	c.Assert(err, ErrorMatches, `layout "/snap/that-snap/current/stuff" defines a layout in space belonging to another snap`)

	const yaml13 = `
name: this-snap
layout:
  $SNAP/relative:
    symlink: $SNAP/existent-dir
`

	// Layout using $SNAP/... as source
	strk = NewScopedTracker()
	info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml13), &SideInfo{Revision: R(42)}, strk)
	c.Assert(err, IsNil)
	c.Assert(info.Layout, HasLen, 1)
	err = ValidateLayoutAll(info)
	c.Assert(err, IsNil)

	var yaml14Pattern = `
name: this-snap
layout:
  %s:
    symlink: $SNAP/existent-dir
`

	// TODO: merge with the block below
	for _, testCase := range []struct {
		str         string
		topLevelDir string
	}{
		{"/nonexistent-dir", "/nonexistent-dir"},
		{"/nonexistent-dir/subdir", "/nonexistent-dir"},
		{"///////unclean-absolute-dir", "/unclean-absolute-dir"},
	} {
		// Layout adding a new top-level directory
		strk = NewScopedTracker()
		yaml14 := fmt.Sprintf(yaml14Pattern, testCase.str)
		info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml14), &SideInfo{Revision: R(42)}, strk)
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 1)
		err = ValidateLayoutAll(info)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`layout %q defines a new top-level directory %q`, testCase.str, testCase.topLevelDir))
	}

	for _, testCase := range []struct {
		str           string
		expectedError string
	}{
		{"$SNAP/with\"quote", "invalid layout path: .* contains a reserved apparmor char.*"},
		{"$SNAP/myDir[0123]", "invalid layout path: .* contains a reserved apparmor char.*"},
		{"$SNAP/here{a,b}", "invalid layout path: .* contains a reserved apparmor char.*"},
		{"$SNAP/anywhere*", "invalid layout path: .* contains a reserved apparmor char.*"},
	} {
		// Layout adding a new top-level directory
		strk = NewScopedTracker()
		yaml14 := fmt.Sprintf(yaml14Pattern, testCase.str)
		info, err = InfoFromSnapYamlWithSideInfo([]byte(yaml14), &SideInfo{Revision: R(42)}, strk)
		c.Assert(err, IsNil)
		c.Assert(info.Layout, HasLen, 1)
		err = ValidateLayoutAll(info)
		c.Assert(err, ErrorMatches, testCase.expectedError, Commentf("path: %s", testCase.str))
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
	mixedSystemUserDaemons := []byte(`
apps:
 foo:
   daemon: simple
 bar:
   daemon: simple
   daemon-scope: user
   after: [foo]
`)

	tcs := []struct {
		name string
		desc []byte
		err  string
	}{{
		name: "foo after baz",
		desc: fooAfterBaz,
		err:  `invalid definition of application "foo": before/after references a missing application "baz"`,
	}, {
		name: "foo before baz",
		desc: fooBeforeBaz,
		err:  `invalid definition of application "foo": before/after references a missing application "baz"`,
	}, {
		name: "foo not a daemon",
		desc: fooNotADaemon,
		err:  `invalid definition of application "foo": must be a service to define before/after ordering`,
	}, {
		name: "foo wants bar, bar not a daemon",
		desc: fooBarNotADaemon,
		err:  `invalid definition of application "foo": before/after references a non-service application "bar"`,
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
		err:  `applications are part of a before/after cycle: foo`,
	}, {
		name: "user daemon wants system daemon",
		desc: mixedSystemUserDaemons,
		err:  `invalid definition of application "bar": before/after references service with different daemon-scope "foo"`,
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

func (s *ValidateSuite) TestValidateAppWatchdogTimeout(c *C) {
	s.testValidateAppTimeout(c, "watchdog")
}
func (s *ValidateSuite) TestValidateAppStartTimeout(c *C) {
	s.testValidateAppTimeout(c, "start")
}
func (s *ValidateSuite) TestValidateAppStopTimeout(c *C) {
	s.testValidateAppTimeout(c, "stop")
}

func (s *ValidateSuite) testValidateAppTimeout(c *C, timeout string) {
	timeout += "-timeout"
	meta := []byte(`
name: foo
version: 1.0
`)
	fooAllGood := []byte(fmt.Sprintf(`
apps:
  foo:
    daemon: simple
    %s: 12s
`, timeout))
	fooNotADaemon := []byte(fmt.Sprintf(`
apps:
  foo:
    %s: 12s
`, timeout))

	fooNegative := []byte(fmt.Sprintf(`
apps:
  foo:
    daemon: simple
    %s: -12s
`, timeout))

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
		err:  timeout + ` is only applicable to services`,
	}, {
		name: "negative timeout",
		desc: fooNegative,
		err:  timeout + ` cannot be negative`,
	}}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)
		c.Assert(info, NotNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, `invalid definition of application "foo": `+tc.err)
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
		err:  `timer is only applicable to services`,
	}, {
		name: "invalid timer",
		desc: badTimer,
		err:  `timer has invalid format: cannot parse "mon2-wed3": invalid schedule fragment`,
	}}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, `invalid definition of application "foo": `+tc.err)
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

func (s *ValidateSuite) TestValidateOsCanHaveBaseNone(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
type: os
base: none
`))
	c.Assert(err, IsNil)
	c.Assert(Validate(info), IsNil)
}

func (s *ValidateSuite) TestValidateBaseInorrectSnapName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
base: aAAAA
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid base name: invalid snap name: \"aAAAA\"`)
}

func (s *ValidateSuite) TestValidateBaseSnapInstanceNameNotAllowed(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
base: foo_abc
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `base cannot specify a snap instance name: "foo_abc"`)
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

func (s *ValidateSuite) TestValidateBaseCanHaveBaseNone(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
type: base
base: none
`))
	c.Assert(err, IsNil)
	c.Assert(Validate(info), IsNil)
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

func (s *validateSuite) TestValidateDescription(c *C) {
	for _, s := range []string{
		"xx", // boringest ASCII
		"🐧🐧", // len("🐧🐧") == 8
		"á", // á (combining)
	} {
		c.Check(ValidateDescription(s), IsNil)
		c.Check(ValidateDescription(strings.Repeat(s, 2049)), ErrorMatches, `description can have up to 4096 codepoints, got 4098`)
		c.Check(ValidateDescription(strings.Repeat(s, 2048)), IsNil)
	}
}

func (s *validateSuite) TestValidateTitle(c *C) {
	for _, s := range []string{
		"xx", // boringest ASCII
		"🐧🐧", // len("🐧🐧") == 8
		"á", // á (combining)
	} {
		c.Check(ValidateTitle(strings.Repeat(s, 21)), ErrorMatches, `title can have up to 40 codepoints, got 42`)
		c.Check(ValidateTitle(strings.Repeat(s, 20)), IsNil)
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
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid instance key: %q`, s))
	}
}

func (s *ValidateSuite) TestValidateAppRestart(c *C) {
	meta := []byte(`
name: foo
version: 1.0
`)
	fooAllGood := []byte(`
apps:
  foo:
    daemon: simple
    restart-condition: on-abort
    restart-delay: 12s
`)
	fooAllGoodDefault := []byte(`
apps:
  foo:
    daemon: simple
`)
	fooAllGoodJustDelay := []byte(`
apps:
  foo:
    daemon: simple
    restart-delay: 12s
`)
	fooConditionNotADaemon := []byte(`
apps:
  foo:
    restart-condition: on-abort
`)
	fooDelayNotADaemon := []byte(`
apps:
  foo:
    restart-delay: 12s
`)
	fooNegativeDelay := []byte(`
apps:
  foo:
    daemon: simple
    restart-delay: -12s
`)

	tcs := []struct {
		name string
		desc []byte
		err  string
	}{{
		name: "foo all good",
		desc: fooAllGood,
	}, {
		name: "foo all good with default values",
		desc: fooAllGoodDefault,
	}, {
		name: "foo all good with restart-delay only",
		desc: fooAllGoodJustDelay,
	}, {
		name: "foo restart-delay but not a service",
		desc: fooDelayNotADaemon,
		err:  `restart-delay is only applicable to services`,
	}, {
		name: "foo restart-delay but not a service",
		desc: fooConditionNotADaemon,
		err:  `restart-condition is only applicable to services`,
	}, {
		name: "negative restart-delay",
		desc: fooNegativeDelay,
		err:  `restart-delay cannot be negative`,
	}}
	for _, tc := range tcs {
		c.Logf("trying %q", tc.name)
		info, err := InfoFromSnapYaml(append(meta, tc.desc...))
		c.Assert(err, IsNil)
		c.Assert(info, NotNil)

		err = Validate(info)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, `invalid definition of application "foo": `+tc.err)
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (s *ValidateSuite) TestValidateSystemUsernames(c *C) {
	const yaml1 = `name: binary
version: 1.0
system-usernames:
  "b@d": shared
`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml1), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.SystemUsernames, HasLen, 1)
	err = Validate(info)
	c.Assert(err, ErrorMatches, `invalid system username "b@d"`)
}

func (s *ValidateSuite) TestValidateSystemUsernamesHappy(c *C) {
	const yaml1 = `name: binary
version: 1.0
system-usernames:
  "snap_daemon": shared
  "_daemon_": shared
`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml1), nil, strk)
	c.Assert(err, IsNil)
	c.Assert(info.SystemUsernames, HasLen, 2)
	err = Validate(info)
	c.Assert(err, IsNil)
}

const yamlNeedDf = `name: need-df
version: 1.0
plugs:
  gtk-3-themes:
    interface: content
    content: gtk-3-themes
    default-provider: gtk-common-themes
  icon-themes:
    interface: content
    content: icon-themes
    default-provider: gtk-common-themes
  other-content:
    interface: content
    content: other
    # no default provider
  # unrelated plug
  network:
`

func (s *ValidateSuite) TestNeededDefaultProviders(c *C) {
	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	dps := NeededDefaultProviders(info)
	c.Check(dps, DeepEquals, map[string][]string{"gtk-common-themes": {"gtk-3-themes", "icon-themes"}})
}

const yamlNeedDfWithSlot = `name: need-df
version: 1.0
plugs:
  gtk-3-themes:
    interface: content
    default-provider: gtk-common-themes2:with-slot
`

func (s *ValidateSuite) TestNeededDefaultProvidersLegacyColonSyntax(c *C) {
	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDfWithSlot), nil, strk)
	c.Assert(err, IsNil)

	dps := NeededDefaultProviders(info)
	c.Check(dps, DeepEquals, map[string][]string{"gtk-common-themes2": {""}})
}

func (s *validateSuite) TestValidateSnapMissingCore(c *C) {
	const yaml = `name: some-snap
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	infos := []*Info{info}
	warns, errors := ValidateBasesAndProviders(infos)
	c.Assert(warns, HasLen, 0)
	c.Assert(errors, HasLen, 1)
	c.Assert(errors[0], ErrorMatches, `cannot use snap "some-snap": required snap "core" missing`)
}

func (s *validateSuite) TestValidateSnapMissingBase(c *C) {
	const yaml = `name: some-snap
base: some-base
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	infos := []*Info{info}
	warns, errors := ValidateBasesAndProviders(infos)
	c.Assert(warns, HasLen, 0)
	c.Assert(errors, HasLen, 1)
	c.Assert(errors[0], ErrorMatches, `cannot use snap "some-snap": base "some-base" is missing`)
}

func (s *validateSuite) TestValidateSnapMissingDefaultProvider(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	var coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	infos := []*Info{snapInfo, coreInfo}
	warns, errors := ValidateBasesAndProviders(infos)
	c.Check(warns, HasLen, 0)
	c.Assert(errors, HasLen, 2)
	c.Check(errors[0], ErrorMatches, `cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "gtk-3-themes" is missing`)
	c.Check(errors[1], ErrorMatches, `cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "icon-themes" is missing`)
}

func (s *validateSuite) TestValidateSnapBaseNoneOK(c *C) {
	const yaml = `name: some-snap
base: none
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	infos := []*Info{info}
	warns, errors := ValidateBasesAndProviders(infos)
	c.Assert(warns, HasLen, 0)
	c.Assert(errors, IsNil)
}

func (s *validateSuite) TestValidateSnapSnapd(c *C) {
	const yaml = `name: snapd
type: snapd
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	infos := []*Info{info}
	warns, errors := ValidateBasesAndProviders(infos)
	c.Assert(warns, HasLen, 0)
	c.Assert(errors, IsNil)
}

func (s *validateSuite) TestValidateDesktopPrefix(c *C) {
	// these are extensively tested elsewhere, so just try some common ones
	for i, tc := range []struct {
		prefix string
		exp    bool
	}{
		{"good", true},
		{"also-good", true},
		{"also-good+instance", true},
		{"", false},
		{"+", false},
		{"@", false},
		{"+good", false},
		{"good+", false},
		{"good+@", false},
		{"old-style_instance", false},
		{"bad+bad+bad", false},
	} {
		c.Logf("tc #%v", i)
		res := ValidateDesktopPrefix(tc.prefix)
		c.Check(res, Equals, tc.exp)
	}
}

func (s *ValidateSuite) TestAppInstallMode(c *C) {
	// check services
	for _, t := range []struct {
		installMode string
		ok          bool
	}{
		// good
		{"", true},
		{"disable", true},
		{"enable", true},
		// bad
		{"invalid-thing", false},
	} {
		err := ValidateApp(&AppInfo{Name: "foo", Daemon: "simple", DaemonScope: SystemDaemon, InstallMode: t.installMode})
		if t.ok {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`"install-mode" field contains invalid value %q`, t.installMode))
		}
	}

	// non-services cannot have a install-mode
	err := ValidateApp(&AppInfo{Name: "foo", Daemon: "", InstallMode: "disable"})
	c.Check(err, ErrorMatches, `"install-mode" cannot be used for "foo", only for services`)
}

func (s *ValidateSuite) TestValidateLinks(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0

links:
 donations:
   - https://donate.me
 contact:
   - me@toto.space
   - https://toto.space
   - mailto:me+support@toto.space
 bug-url:
   - https://github.com/webteam-space/toto.space/issues
 website:
   - https://toto.space
 source-code:
   - https://github.com/webteam-space/toto.space
`))
	c.Assert(err, IsNil)

	// happy
	err = Validate(info)
	c.Assert(err, IsNil)

	info, err = InfoFromSnapYaml([]byte(`name: foo
version: 1.0
links:
  foo:
   - ""
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `empty "foo" link`)

	info, err = InfoFromSnapYaml([]byte(`name: foo
version: 1.0
links:
  foo:
   - ":"
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid "foo" link ":"`)

	info, err = InfoFromSnapYaml([]byte(`name: foo
version: 1.0
links:
  foo: []
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `"foo" links cannot be specified and empty`)
}

func (s *YamlSuite) TestValidateLinksKeys(c *C) {
	invalid := []string{
		"--",
		"1-2",
		"aa-",
		"",
	}

	for _, k := range invalid {
		links := map[string][]string{
			k: {"link"},
		}
		err := ValidateLinks(links)
		if k == "" {
			c.Check(err, ErrorMatches, "links key cannot be empty")
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`links key is invalid: %s`, k))
		}
	}
}

func (s *YamlSuite) TestValidateLinksValues(c *C) {
	invalid := []struct {
		link string
		err  string
	}{
		{"foo:bar", `"contact" link must have one of http|https schemes or it must be an email address: "foo:bar"`},
		{"a", `invalid "contact" email address "a"`},
		{":", `invalid "contact" link ":"`},
		{"", `empty "contact" link`},
	}

	for _, l := range invalid {
		links := map[string][]string{
			"contact": {l.link},
		}
		err := ValidateLinks(links)
		c.Check(err, ErrorMatches, l.err)
	}
}

func (s *ValidateSuite) TestSimplePrereqTracker(c *C) {
	// check that it implements the needed interface
	var _ snapstate.PrereqTracker = SimplePrereqTracker{}

	info := &Info{
		Plugs: map[string]*PlugInfo{},
	}
	info.Plugs["foo"] = &PlugInfo{
		Snap:      info,
		Name:      "sound-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "common-themes", "content": "foo"},
	}
	info.Plugs["bar"] = &PlugInfo{
		Snap:      info,
		Name:      "visual-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "common-themes", "content": "bar"},
	}
	info.Plugs["baz"] = &PlugInfo{
		Snap:      info,
		Name:      "not-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"default-provider": "some-snap", "content": "baz"},
	}
	info.Plugs["qux"] = &PlugInfo{Snap: info, Interface: "not-content"}

	repo := interfaces.NewRepository()

	prqt := SimplePrereqTracker{}
	providerContentAttrs := prqt.MissingProviderContentTags(info, repo)
	c.Check(providerContentAttrs, HasLen, 2)
	sort.Strings(providerContentAttrs["common-themes"])
	c.Check(providerContentAttrs["common-themes"], DeepEquals, []string{"bar", "foo"})
	c.Check(providerContentAttrs["some-snap"], DeepEquals, []string{"baz"})

	for _, i := range builtin.Interfaces() {
		c.Assert(repo.AddInterface(i), IsNil)
	}

	slotSnap := &Info{SuggestedName: "slot-snap"}
	barSlot := &SlotInfo{
		Snap:      slotSnap,
		Name:      "visual-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"content": "bar"},
	}
	err := repo.AddSlot(barSlot)
	c.Assert(err, IsNil)
	providerContentAttrs = prqt.MissingProviderContentTags(info, repo)
	c.Check(providerContentAttrs, HasLen, 2)
	c.Check(providerContentAttrs["common-themes"], DeepEquals, []string{"foo"})
	// stays the same
	c.Check(providerContentAttrs["some-snap"], DeepEquals, []string{"baz"})

	fooSlot := &SlotInfo{
		Snap:      slotSnap,
		Name:      "sound-themes",
		Interface: "content",
		Attrs:     map[string]interface{}{"content": "foo"},
	}
	err = repo.AddSlot(fooSlot)
	c.Assert(err, IsNil)
	providerContentAttrs = prqt.MissingProviderContentTags(info, repo)
	c.Check(providerContentAttrs, HasLen, 1)
	c.Check(providerContentAttrs["common-themes"], IsNil)
	// stays the same
	c.Check(providerContentAttrs["some-snap"], DeepEquals, []string{"baz"})

	// no repo => no filtering
	providerContentAttrs = prqt.MissingProviderContentTags(info, nil)
	c.Check(providerContentAttrs, HasLen, 2)
	sort.Strings(providerContentAttrs["common-themes"])
	c.Check(providerContentAttrs["common-themes"], DeepEquals, []string{"bar", "foo"})
	c.Check(providerContentAttrs["some-snap"], DeepEquals, []string{"baz"})
}

func (s *ValidateSuite) TestSelfContainedSetPrereqTrackerBasics(c *C) {
	// check that it implements the needed interface
	var prqt snapstate.PrereqTracker = NewSelfContainedSetPrereqTracker()

	// this ignores arguments and always returns nil
	c.Check(prqt.MissingProviderContentTags(nil, nil), IsNil)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerSnaps(c *C) {
	prqt := NewSelfContainedSetPrereqTracker()

	// snap-1 here is twice to ensure it is de-duped
	for _, sn := range []string{"snap-1", "snap-1", "snap-2"} {
		yaml := fmt.Sprintf(`name: %s
base: some-base
version: 1.0`, sn)
		info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, NewScopedTracker())
		c.Assert(err, IsNil)

		prqt.Add(info)
	}

	snaps := prqt.Snaps()
	c.Assert(snaps, HasLen, 2)

	c.Check(snaps[0].SuggestedName, Equals, "snap-1")
	c.Check(snaps[1].SuggestedName, Equals, "snap-2")
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerMissingBase(c *C) {
	const yaml = `name: some-snap
base: some-base
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(info)

	_, errors := prqt.Check()
	c.Assert(errors, HasLen, 1)
	c.Check(errors[0], ErrorMatches, `cannot use snap "some-snap": base "some-base" is missing`)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerMissingMCore(c *C) {
	const yaml = `name: some-snap
version: 1.0`

	strk := NewScopedTracker()
	info, err := InfoFromSnapYamlWithSideInfo([]byte(yaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(info)

	_, errors := prqt.Check()
	c.Assert(errors, HasLen, 1)
	c.Check(errors[0], ErrorMatches, `cannot use snap "some-snap": required snap "core" missing`)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerMissingDefaultProvider(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(snapInfo)
	prqt.Add(coreInfo)

	_, errors := prqt.Check()
	c.Assert(errors, HasLen, 2)
	c.Check(errors[0], ErrorMatches, `cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "gtk-3-themes" is missing`)
	c.Check(errors[1], ErrorMatches, `cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "icon-themes" is missing`)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerDefaultProviderHappy(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	const gtkCommonThemesYaml = `name: gtk-common-themes
version: 1.0
slots:
  gtk-3-themes:
    interface: content
    read: [$SNAP/themes]
  icon-themes:
    interface: content
    read: [$SNAP/themes]
`
	defer MockSanitizePlugsSlots(builtin.SanitizePlugsSlots)()

	gtkCommonThemesInfo, err := InfoFromSnapYamlWithSideInfo([]byte(gtkCommonThemesYaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(snapInfo)
	prqt.Add(coreInfo)
	prqt.Add(gtkCommonThemesInfo)

	warns, errors := prqt.Check()
	c.Assert(errors, HasLen, 0)
	c.Assert(warns, HasLen, 0)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerAlternativeProviders(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	const iconsProviderYaml = `name: icons-provider
version: 1.0
slots:
  serve-icon-themes:
    interface: content
    content: icon-themes
`
	const themesProviderYaml = `name: themes-provider
version: 1.0
slots:
  serve-gtk-3-themes:
    interface: content
    content: gtk-3-themes
`
	iconsProviderInfo, err := InfoFromSnapYamlWithSideInfo([]byte(iconsProviderYaml), nil, strk)
	c.Assert(err, IsNil)
	themesProviderInfo, err := InfoFromSnapYamlWithSideInfo([]byte(themesProviderYaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(snapInfo)
	prqt.Add(coreInfo)
	prqt.Add(iconsProviderInfo)
	prqt.Add(themesProviderInfo)

	warns, errors := prqt.Check()
	c.Assert(errors, HasLen, 0)
	c.Assert(warns, HasLen, 2)
	c.Check(warns[0], ErrorMatches, `snap "need-df" requires a provider for content "gtk-3-themes", a candidate slot is available \(themes-provider:serve-gtk-3-themes\) but not the default-provider, ensure a single auto-connection \(or possibly a connection\) is in-place`)
	c.Check(warns[1], ErrorMatches, `snap "need-df" requires a provider for content "icon-themes", a candidate slot is available \(icons-provider:serve-icon-themes\) but not the default-provider, ensure a single auto-connection \(or possibly a connection\) is in-place`)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerDefaultProviderAlternativeProviderThroughValidateBasesAndProviders(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	const gtkCommonThemesYaml = `name: gtk-common-themes
version: 1.0
slots:
  gtk-3-themes:
    interface: content
    read: [$SNAP/themes]
  icon-themes:
    interface: content
    read: [$SNAP/themes]
`
	const themesProvider2Yaml = `name: themes-provider
version: 1.0
slots:
  serve-gtk-3-themes:
    interface: content
    content: gtk-3-themes
    read: [$SNAP/stuff]
`
	defer MockSanitizePlugsSlots(builtin.SanitizePlugsSlots)()

	gtkCommonThemesInfo, err := InfoFromSnapYamlWithSideInfo([]byte(gtkCommonThemesYaml), nil, strk)
	c.Assert(err, IsNil)
	themesProvider2Info, err := InfoFromSnapYamlWithSideInfo([]byte(themesProvider2Yaml), nil, strk)
	c.Assert(err, IsNil)

	warns, errors := ValidateBasesAndProviders([]*Info{snapInfo, coreInfo, gtkCommonThemesInfo, themesProvider2Info})
	c.Assert(errors, HasLen, 0)
	c.Assert(warns, HasLen, 1)
	c.Check(warns[0], ErrorMatches, `snap "need-df" requires a provider for content "gtk-3-themes", many candidates slots are available \(themes-provider:serve-gtk-3-themes\) including from default-provider gtk-common-themes:gtk-3-themes, ensure a single auto-connection \(or possibly a connection\) is in-place`)
}

func (s *validateSuite) TestSelfContainedSetPrereqTrackerDoubleAlternativeProviders(c *C) {
	strk := NewScopedTracker()
	snapInfo, err := InfoFromSnapYamlWithSideInfo([]byte(yamlNeedDf), nil, strk)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os`

	coreInfo, err := InfoFromSnapYamlWithSideInfo([]byte(coreYaml), nil, strk)
	c.Assert(err, IsNil)

	const iconsProviderYaml = `name: icons-provider
version: 1.0
slots:
  serve-icon-themes:
    interface: content
    content: icon-themes
    read: [$SNAP/stuff]
`
	const themesProviderYaml = `name: themes-provider
version: 1.0
slots:
  serve-gtk-3-themes:
    interface: content
    content: gtk-3-themes
    read: [$SNAP/stuff]
`
	const themesProvider2Yaml = `name: themes-provider2
version: 1.1
slots:
  gtk-3-themes:
    interface: content
    read: [$SNAP/stuff]
  unrelated-slot:
    interface: dbus
    bus: system
    name: foo.Foo
`
	defer MockSanitizePlugsSlots(builtin.SanitizePlugsSlots)()

	iconsProviderInfo, err := InfoFromSnapYamlWithSideInfo([]byte(iconsProviderYaml), nil, strk)
	c.Assert(err, IsNil)
	themesProviderInfo, err := InfoFromSnapYamlWithSideInfo([]byte(themesProviderYaml), nil, strk)
	c.Assert(err, IsNil)
	themesProvider2Info, err := InfoFromSnapYamlWithSideInfo([]byte(themesProvider2Yaml), nil, strk)
	c.Assert(err, IsNil)

	prqt := NewSelfContainedSetPrereqTracker()
	prqt.Add(snapInfo)
	prqt.Add(coreInfo)
	prqt.Add(iconsProviderInfo)
	prqt.Add(themesProviderInfo)
	prqt.Add(themesProvider2Info)

	warns, errors := prqt.Check()
	c.Assert(errors, HasLen, 0)
	c.Assert(warns, HasLen, 2)
	c.Check(warns[0], ErrorMatches, `snap "need-df" requires a provider for content "gtk-3-themes", many candidate slots are available \(themes-provider2:gtk-3-themes, themes-provider:serve-gtk-3-themes\) but not the default-provider, ensure a single auto-connection \(or possibly a connection\) is in-place`)
	c.Check(warns[1], ErrorMatches, `snap "need-df" requires a provider for content "icon-themes", a candidate slot is available \(icons-provider:serve-icon-themes\) but not the default-provider, ensure a single auto-connection \(or possibly a connection\) is in-place`)
}

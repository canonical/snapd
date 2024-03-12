// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2021 Canonical Ltd
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

package servicestate_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type statusDecoratorSuite struct{}

var _ = Suite(&statusDecoratorSuite{})

func (s *statusDecoratorSuite) TestDecorateWithStatus(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")
	snp := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(1),
		},
	}
	err := os.MkdirAll(snp.MountDir(), 0755)
	c.Assert(err, IsNil)
	err = os.Symlink(snp.Revision.String(), filepath.Join(filepath.Dir(snp.MountDir()), "current"))
	c.Assert(err, IsNil)

	disabled := false
	r := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		switch args[0] {
		case "show":
			c.Assert(args[0], Equals, "show")
			unit := args[2]
			activeState, unitState := "active", "enabled"
			if disabled {
				activeState = "inactive"
				unitState = "disabled"
			}
			if strings.HasSuffix(unit, ".timer") || strings.HasSuffix(unit, ".socket") || strings.HasSuffix(unit, ".target") {
				// Units using the baseProperties query
				return []byte(fmt.Sprintf(`Id=%s
Names=%[1]s
ActiveState=%s
UnitFileState=%s
`, args[2], activeState, unitState)), nil
			} else {
				// Units using the extendedProperties query
				return []byte(fmt.Sprintf(`Id=%s
Names=%[1]s
Type=simple
ActiveState=%s
UnitFileState=%s
NeedDaemonReload=no
`, args[2], activeState, unitState)), nil
			}
		case "--user":
			c.Assert(args[1], Equals, "--global")
			c.Assert(args[2], Equals, "is-enabled")
			unitState := "enabled\n"
			if disabled {
				unitState = "disabled\n"
			}
			return bytes.Repeat([]byte(unitState), len(args)-3), nil
		default:
			c.Errorf("unexpected systemctl command: %v", args)
			return nil, fmt.Errorf("should not be reached")
		}
	})
	defer r()

	sd := servicestate.NewStatusDecorator(nil)

	// not a service
	app := &client.AppInfo{
		Snap: "foo",
		Name: "app",
	}
	snapApp := &snap.AppInfo{Snap: snp, Name: "app"}

	err = sd.DecorateWithStatus(app, snapApp)
	c.Assert(err, IsNil)

	for _, enabled := range []bool{true, false} {
		disabled = !enabled

		// service only
		app = &client.AppInfo{
			Snap:   snp.InstanceName(),
			Name:   "svc",
			Daemon: "simple",
		}
		snapApp = &snap.AppInfo{
			Snap:        snp,
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, enabled)
		c.Check(app.Enabled, Equals, enabled)

		// service  + timer
		app = &client.AppInfo{
			Snap:   snp.InstanceName(),
			Name:   "svc",
			Daemon: "simple",
		}
		snapApp = &snap.AppInfo{
			Snap:        snp,
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}
		snapApp.Timer = &snap.TimerInfo{
			App:   snapApp,
			Timer: "10:00",
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, enabled)
		c.Check(app.Enabled, Equals, enabled)
		c.Check(app.Activators, DeepEquals, []client.AppActivator{
			{Name: "svc", Type: "timer", Active: enabled, Enabled: enabled},
		})

		// service with socket
		app = &client.AppInfo{
			Snap:   snp.InstanceName(),
			Name:   "svc",
			Daemon: "simple",
		}
		snapApp = &snap.AppInfo{
			Snap:        snp,
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}
		snapApp.Sockets = map[string]*snap.SocketInfo{
			"socket1": {
				App:          snapApp,
				Name:         "socket1",
				ListenStream: "a.socket",
			},
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, enabled)
		c.Check(app.Enabled, Equals, enabled)
		c.Check(app.Activators, DeepEquals, []client.AppActivator{
			{Name: "socket1", Type: "socket", Active: enabled, Enabled: enabled},
		})

		// service with slot activation will always be enabled as we cannot
		// disable/enable slot activation at the moment.
		app = &client.AppInfo{
			Snap:   snp.InstanceName(),
			Name:   "svc",
			Daemon: "simple",
		}
		snapApp = &snap.AppInfo{
			Snap:        snp,
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}
		snapApp.ActivatesOn = []*snap.SlotInfo{
			{
				Snap:      snp,
				Name:      "dbus-slot",
				Interface: "dbus",
				Attrs: map[string]interface{}{
					"bus":  "system",
					"name": "org.example.Svc",
				},
			},
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, enabled)
		c.Check(app.Enabled, Equals, true)
		c.Check(app.Activators, DeepEquals, []client.AppActivator{
			{Name: "org.example.Svc", Type: "dbus", Active: true, Enabled: true},
		})

		// No state is currently extracted for user daemons
		app = &client.AppInfo{
			Snap:   snp.InstanceName(),
			Name:   "svc",
			Daemon: "simple",
		}
		snapApp = &snap.AppInfo{
			Snap:        snp,
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		}
		snapApp.Sockets = map[string]*snap.SocketInfo{
			"socket1": {
				App:          snapApp,
				Name:         "socket1",
				ListenStream: "a.socket",
			},
		}
		snapApp.Timer = &snap.TimerInfo{
			App:   snapApp,
			Timer: "10:00",
		}
		snapApp.ActivatesOn = []*snap.SlotInfo{
			{
				Snap:      snp,
				Name:      "dbus-slot",
				Interface: "dbus",
				Attrs: map[string]interface{}{
					"bus":  "session",
					"name": "org.example.Svc",
				},
			},
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, false)
		c.Check(app.Enabled, Equals, true) // when a service is slot activated its always enabled
		c.Check(app.Activators, DeepEquals, []client.AppActivator{
			{Name: "socket1", Type: "socket", Active: false, Enabled: enabled},
			{Name: "svc", Type: "timer", Active: false, Enabled: enabled},
			{Name: "org.example.Svc", Type: "dbus", Active: true, Enabled: true},
		})
	}
}

type userSelectorSuite struct{}

var _ = Suite(&userSelectorSuite{})

func (s *userSelectorSuite) TestUserScopeMarshalListOfUsernames(c *C) {
	us := servicestate.UserSelector{
		Names: []string{"user", "user-two"},
	}
	b, err := json.Marshal(us)
	c.Assert(err, IsNil)
	c.Check(string(b), Equals, `["user","user-two"]`)
}

func (s *userSelectorSuite) TestUserScopeMarshalStringKeyword(c *C) {
	us := servicestate.UserSelector{
		Selector: servicestate.UserSelectionSelf,
	}
	b, err := json.Marshal(us)
	c.Assert(err, IsNil)
	c.Check(string(b), Equals, `"self"`)
}

func (s *userSelectorSuite) TestUserScopeMarshalInvalidSelector(c *C) {
	us := servicestate.UserSelector{
		Selector: 42,
	}
	_, err := json.Marshal(us)
	c.Assert(err, ErrorMatches, `.* internal error: unsupported selector 42 specified`)
}

func (s *userSelectorSuite) TestUserScopeUnmarshalInvalidType(c *C) {
	const userScopeJson = `1`
	var us servicestate.UserSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, ErrorMatches, `cannot unmarshal, expected a string or a list of strings`)
}

func (s *userSelectorSuite) TestUserScopeUnmarshalListOfUsernames(c *C) {
	const userScopeJson = `["my-user","other-user"]`
	var us servicestate.UserSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.UserSelector{
		Names: []string{"my-user", "other-user"},
	})
}

func (s *userSelectorSuite) TestUserScopeUnmarshalStringKeyword(c *C) {
	const userScopeJson = `"all"`
	var us servicestate.UserSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.UserSelector{
		Selector: servicestate.UserSelectionAll,
	})
}

func (s *userSelectorSuite) TestUserListCurrentUser(c *C) {
	us := servicestate.UserSelector{
		Selector: servicestate.UserSelectionSelf,
	}

	users, err := us.UserList(&user.User{
		Uid:      "1000",
		Username: "my-user",
	})
	c.Assert(err, IsNil)
	c.Check(users, DeepEquals, []string{"my-user"})
}

func (s *userSelectorSuite) TestUserListCurrentUserInvalidNil(c *C) {
	us := servicestate.UserSelector{
		Selector: servicestate.UserSelectionSelf,
	}

	users, err := us.UserList(nil)
	c.Assert(err, ErrorMatches, `internal error: for "self" the current user must be provided`)
	c.Check(users, IsNil)
}

func (s *userSelectorSuite) TestUserListCurrentUserNotValidForRoot(c *C) {
	us := servicestate.UserSelector{
		Selector: servicestate.UserSelectionSelf,
	}

	users, err := us.UserList(&user.User{
		Uid:      "0",
		Username: "my-user",
	})
	c.Assert(err, ErrorMatches, `cannot use "self" for root user`)
	c.Check(users, IsNil)
}

func (s *userSelectorSuite) TestUserListInvalidSelector(c *C) {
	us := servicestate.UserSelector{
		Selector: 42,
	}

	users, err := us.UserList(nil)
	c.Assert(err, ErrorMatches, `internal error: unsupported selector 42 specified`)
	c.Check(users, IsNil)
}

func (s *userSelectorSuite) TestUserListUsersReturnsEmpty(c *C) {
	us := servicestate.UserSelector{
		Selector: servicestate.UserSelectionAll,
	}

	users, err := us.UserList(nil)
	c.Assert(err, IsNil)
	c.Check(users, IsNil)
}

type scopeSelectorSuite struct{}

var _ = Suite(&scopeSelectorSuite{})

func (s *scopeSelectorSuite) TestScopeUnmarshalInvalidType(c *C) {
	const userScopeJson = `1`
	var us servicestate.ScopeSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, ErrorMatches, `cannot unmarshal, expected a list of strings`)
}

func (s *scopeSelectorSuite) TestScopeUnmarshalInvalidKeyword(c *C) {
	const userScopeJson = `["all"]`
	var us servicestate.ScopeSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, ErrorMatches, `cannot unmarshal, expected one of: "system", "user"`)
}

func (s *scopeSelectorSuite) TestScopeUnmarshalNone(c *C) {
	const userScopeJson = `[]`
	var us servicestate.ScopeSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.ScopeSelector{})
}

func (s *scopeSelectorSuite) TestScopeUnmarshalSystem(c *C) {
	const userScopeJson = `["system"]`
	var us servicestate.ScopeSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.ScopeSelector{"system"})
}

func (s *scopeSelectorSuite) TestScopeUnmarshalUser(c *C) {
	const userScopeJson = `["user"]`
	var us servicestate.ScopeSelector
	err := json.Unmarshal([]byte(userScopeJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.ScopeSelector{"user"})
}

type instructionSuite struct {
	rootUser       *user.User
	defaultUser    *user.User
	systemServices []*snap.AppInfo
	mixServices    []*snap.AppInfo
}

var _ = Suite(&instructionSuite{})

func (s *instructionSuite) SetUpTest(c *C) {
	s.rootUser = &user.User{
		Username: "my-root",
		Uid:      "0",
	}
	s.defaultUser = &user.User{
		Username: "my-user",
		Uid:      "1000",
	}

	s.systemServices = []*snap.AppInfo{
		{
			Name:        "foo",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		},
		{
			Name:        "bar",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		},
	}
	s.mixServices = []*snap.AppInfo{
		{
			Name:        "foo",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		{
			Name:        "bar",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		},
	}
}

func (s *instructionSuite) TestUnmarshalEmpty(c *C) {
	const instJson = `{}`
	var us servicestate.Instruction
	err := json.Unmarshal([]byte(instJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.Instruction{})

	// Scope and users has custom unmarshal logic, test they are set
	// to expected empty values
	c.Check(us.Scope, HasLen, 0)
	c.Check(us.Users.Selector, Equals, servicestate.UserSelectionList)
	c.Check(us.Users.Names, HasLen, 0)
}

func (s *instructionSuite) TestUnmarshalSimple(c *C) {
	const instJson = `{"action":"start", "names":["svc1", "svc2"], "enable": true}`
	var us servicestate.Instruction
	err := json.Unmarshal([]byte(instJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.Instruction{
		Action: "start",
		Names:  []string{"svc1", "svc2"},
		StartOptions: client.StartOptions{
			Enable: true,
		},
	})

	// Scope and users has custom unmarshal logic, test they are set
	// to expected empty values
	c.Check(us.Scope, HasLen, 0)
	c.Check(us.Users.Selector, Equals, servicestate.UserSelectionList)
	c.Check(us.Users.Names, HasLen, 0)
}

func (s *instructionSuite) TestUnmarshalWithScopes(c *C) {
	const instJson = `{"action":"restart", "names":["svc1"], "reload": true, "scope": ["user"], "users": "all"}`
	var us servicestate.Instruction
	err := json.Unmarshal([]byte(instJson), &us)
	c.Assert(err, IsNil)
	c.Check(us, DeepEquals, servicestate.Instruction{
		Action: "restart",
		Names:  []string{"svc1"},
		RestartOptions: client.RestartOptions{
			Reload: true,
		},
		Scope: []string{"user"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionAll,
		},
	})
}

func (s *instructionSuite) TestEnsureDefaultScopeForUserDefaultRoot(c *C) {
	inst := &servicestate.Instruction{}
	inst.EnsureDefaultScopeForUser(s.rootUser)
	c.Check(inst.Scope, DeepEquals, servicestate.ScopeSelector{"system", "user"})
}

func (s *instructionSuite) TestEnsureDefaultScopeForUserAlreadySetDoesNothingRoot(c *C) {
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"system"}}
	inst.EnsureDefaultScopeForUser(s.rootUser)
	c.Check(inst.Scope, DeepEquals, servicestate.ScopeSelector{"system"})
}

func (s *instructionSuite) TestEnsureDefaultScopeForUserDefaultNonRoot(c *C) {
	inst := &servicestate.Instruction{}
	inst.EnsureDefaultScopeForUser(s.defaultUser)
	c.Check(inst.Scope, DeepEquals, servicestate.ScopeSelector{"system"})
}

func (s *instructionSuite) TestEnsureDefaultScopeForUserAlreadySetDoesNothingNonRoot(c *C) {
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"user"}}
	inst.EnsureDefaultScopeForUser(s.defaultUser)
	c.Check(inst.Scope, DeepEquals, servicestate.ScopeSelector{"user"})
}

func (s *instructionSuite) TestValidateNoScopesForRootOnlySystemServicesHappy(c *C) {
	inst := &servicestate.Instruction{}
	c.Check(inst.Validate(s.rootUser, s.systemServices), IsNil)
}

func (s *instructionSuite) TestValidateNoScopesForRootMixServicesHappy(c *C) {
	inst := &servicestate.Instruction{}
	c.Check(inst.Validate(s.rootUser, s.mixServices), IsNil)
}

func (s *instructionSuite) TestValidateNoScopesForNonRootOnlySystemServicesHappy(c *C) {
	inst := &servicestate.Instruction{}
	c.Check(inst.Validate(s.defaultUser, s.systemServices), IsNil)
}

func (s *instructionSuite) TestValidateNoScopesForNonRootMixServicesFails(c *C) {
	inst := &servicestate.Instruction{}
	c.Check(inst.Validate(s.defaultUser, s.mixServices), ErrorMatches, `non-root users must specify service scope when targeting user services`)
}

func (s *instructionSuite) TestValidateNoUsersForRootOnlySystemServicesHappy(c *C) {
	// Provide scopes to avoid hitting any checks in validateScope
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"system", "user"}}
	c.Check(inst.Validate(s.rootUser, s.systemServices), IsNil)
}

func (s *instructionSuite) TestValidateNoUsersForRootMixServicesHappy(c *C) {
	// Provide scopes to avoid hitting any checks in validateScope
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"system", "user"}}
	c.Check(inst.Validate(s.rootUser, s.mixServices), IsNil)
}

func (s *instructionSuite) TestValidateNoUsersForNonRootOnlySystemServicesHappy(c *C) {
	// Provide scopes to avoid hitting any checks in validateScope
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"system", "user"}}
	c.Check(inst.Validate(s.defaultUser, s.systemServices), IsNil)
}

func (s *instructionSuite) TestValidateAllUsersForNonRootHappy(c *C) {
	// Provide scopes to avoid hitting any checks in validateScope
	inst := &servicestate.Instruction{
		Scope: servicestate.ScopeSelector{"system", "user"},
		Users: servicestate.UserSelector{Selector: servicestate.UserSelectionAll},
	}
	c.Check(inst.Validate(s.defaultUser, s.mixServices), IsNil)
}

func (s *instructionSuite) TestValidateNoUsersForNonRootMixServicesFails(c *C) {
	// Provide scopes to avoid hitting any checks in validateScope
	inst := &servicestate.Instruction{Scope: servicestate.ScopeSelector{"system", "user"}}
	c.Check(inst.Validate(s.defaultUser, s.mixServices), ErrorMatches, `non-root users must specify users when targeting user services`)
}

type snapServiceOptionsSuite struct {
	testutil.BaseTest
	state *state.State
}

var _ = Suite(&snapServiceOptionsSuite{})

func (s *snapServiceOptionsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.state = state.New(nil)
}

func (s *snapServiceOptionsSuite) TestSnapServiceOptionsVitalityRank(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()
	t := config.NewTransaction(st)
	err := t.Set("core", "resilience.vitality-hint", "bar,foo")
	c.Assert(err, IsNil)
	t.Commit()

	fooInfo := snaptest.MockInfo(c, "name: foo\nversion: 0", nil)
	barInfo := snaptest.MockInfo(c, "name: bar\nversion: 0", nil)
	bazInfo := snaptest.MockInfo(c, "name: baz\nversion: 0", nil)

	opts, err := servicestate.SnapServiceOptions(st, fooInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		VitalityRank: 2,
	})
	opts, err = servicestate.SnapServiceOptions(st, barInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		VitalityRank: 1,
	})
	opts, err = servicestate.SnapServiceOptions(st, bazInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		VitalityRank: 0,
	})
}

func (s *snapServiceOptionsSuite) TestSnapServiceOptionsQuotaGroups(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	fooInfo := snaptest.MockInfo(c, `
name: foosnap
version: 0
`, nil)

	// make a quota group
	grp, err := quota.NewGroup("foogroup", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
	c.Assert(err, IsNil)

	grp.Snaps = []string{"foosnap"}

	// add it into the state
	newGrps, err := servicestatetest.PatchQuotas(st, grp)
	c.Assert(err, IsNil)
	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	opts, err := servicestate.SnapServiceOptions(st, fooInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		QuotaGroup: grp,
	})

	// save the current state of the quota group before modifying it to prove
	// that the group caching works
	grps, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)

	// modify state to use an instance name instead now
	grp.Snaps = []string{"foosnap_instance"}
	newGrps, err = servicestatetest.PatchQuotas(st, grp)
	c.Assert(err, IsNil)
	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// we can still get the quota group using the local map we got before
	// modifying state
	opts, err = servicestate.SnapServiceOptions(st, fooInfo, grps)
	c.Assert(err, IsNil)
	grp.Snaps = []string{"foosnap"}
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		QuotaGroup: grp,
	})

	// but using state produces nothing for the non-instance name snap
	opts, err = servicestate.SnapServiceOptions(st, fooInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{})

	// but it does work with instance names
	fooInfo.InstanceKey = "instance"
	grp.Snaps = []string{"foosnap_instance"}
	opts, err = servicestate.SnapServiceOptions(st, fooInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		QuotaGroup: grp,
	})

	// works with vitality rank for the snap too
	t := config.NewTransaction(st)
	err = t.Set("core", "resilience.vitality-hint", "bar,foosnap_instance")
	c.Assert(err, IsNil)
	t.Commit()

	opts, err = servicestate.SnapServiceOptions(st, fooInfo, nil)
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &wrappers.SnapServiceOptions{
		VitalityRank: 2,
		QuotaGroup:   grp,
	})
}

func (s *snapServiceOptionsSuite) TestServiceControlTaskSummaries(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	si := snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	snp := &snap.Info{SideInfo: si}
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	appInfos := []*snap.AppInfo{
		{
			Snap:        snp,
			Name:        "svc1",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		{
			Snap:        snp,
			Name:        "svc2",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
	}

	for _, tc := range []struct {
		instruction     *servicestate.Instruction
		expectedSummary string
	}{
		{
			&servicestate.Instruction{Action: "start"},
			`Run service command "start" for services ["svc1" "svc2"] of snap "foo"`,
		},
		{
			&servicestate.Instruction{Action: "restart"},
			`Run service command "restart" for running services of snap "foo"`,
		},
		{
			&servicestate.Instruction{
				Action: "restart",
				Names:  []string{"foo.svc2"},
			},
			`Run service command "restart" for services ["svc2"] of snap "foo"`,
		},
		{
			&servicestate.Instruction{
				Action:         "restart",
				RestartOptions: client.RestartOptions{Reload: true},
				Names:          []string{"foo.svc2", "foo.svc1"},
			},
			`Run service command "reload-or-restart" for services ["svc1" "svc2"] of snap "foo"`,
		},
		{
			&servicestate.Instruction{
				Action: "stop",
				Names:  []string{"foo.svc1"},
			},
			`Run service command "stop" for services ["svc1"] of snap "foo"`,
		},
	} {
		taskSet, err := servicestate.ServiceControlTs(st, appInfos, tc.instruction, nil)
		c.Check(err, IsNil)
		tasks := taskSet.Tasks()
		c.Assert(tasks, HasLen, 1)
		task := tasks[0]
		c.Check(task.Summary(), Equals, tc.expectedSummary)
	}
}

func (s *snapServiceOptionsSuite) checkServiceAction(c *C, ts *state.TaskSet, expected *servicestate.ServiceAction) {
	c.Assert(ts.Tasks(), HasLen, 1)
	t := ts.Tasks()[0]

	var obtained servicestate.ServiceAction
	c.Assert(t.Get("service-action", &obtained), IsNil)
	c.Check(&obtained, DeepEquals, expected)
}

func (s *snapServiceOptionsSuite) TestServiceControlServiceAction(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	si := snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	snp := &snap.Info{SideInfo: si}
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	appInfos := []*snap.AppInfo{
		{
			Snap:        snp,
			Name:        "svc1",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		{
			Snap:        snp,
			Name:        "svc2",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
	}

	for _, tc := range []struct {
		instruction    *servicestate.Instruction
		expectedAction *servicestate.ServiceAction
	}{
		{
			&servicestate.Instruction{
				Action: "start",
				Scope:  servicestate.ScopeSelector{"user"},
				Users:  servicestate.UserSelector{Names: []string{"my-user"}},
				StartOptions: client.StartOptions{
					Enable: true,
				},
			},
			&servicestate.ServiceAction{
				SnapName:       "foo",
				Action:         "start",
				ActionModifier: "enable",
				Services:       []string{"svc1", "svc2"},
				ScopeOptions: wrappers.ScopeOptions{
					Scope: "user",
					Users: []string{"my-user"},
				},
			},
		},
		{
			&servicestate.Instruction{
				Action: "restart",
				Scope:  servicestate.ScopeSelector{"user"},
				Users:  servicestate.UserSelector{Names: []string{"foo"}},
			},
			&servicestate.ServiceAction{
				SnapName:                "foo",
				Action:                  "restart",
				Services:                []string{"svc1", "svc2"},
				RestartEnabledNonActive: true,
				ScopeOptions: wrappers.ScopeOptions{
					Scope: "user",
					Users: []string{"foo"},
				},
			},
		},
		{
			&servicestate.Instruction{
				Action: "restart",
				Scope:  servicestate.ScopeSelector{"system"},
				Names:  []string{"foo.svc2"},
			},
			&servicestate.ServiceAction{
				SnapName:                "foo",
				Action:                  "restart",
				Services:                []string{"svc1", "svc2"},
				ExplicitServices:        []string{"svc2"},
				RestartEnabledNonActive: true,
				ScopeOptions: wrappers.ScopeOptions{
					Scope: "system",
				},
			},
		},
		{
			&servicestate.Instruction{
				Action:         "restart",
				RestartOptions: client.RestartOptions{Reload: true},
				Names:          []string{"foo.svc2", "foo.svc1"},
				Scope:          servicestate.ScopeSelector{"system", "user"},
				Users:          servicestate.UserSelector{Names: []string{"foo"}},
			},
			&servicestate.ServiceAction{
				SnapName:                "foo",
				Action:                  "reload-or-restart",
				Services:                []string{"svc1", "svc2"},
				ExplicitServices:        []string{"svc1", "svc2"},
				RestartEnabledNonActive: true,
				ScopeOptions: wrappers.ScopeOptions{
					Users: []string{"foo"},
				},
			},
		},
		{
			&servicestate.Instruction{
				Action: "stop",
				Names:  []string{"foo.svc1"},
				Scope:  servicestate.ScopeSelector{"user"},
				Users:  servicestate.UserSelector{Names: []string{"baz"}},
			},
			&servicestate.ServiceAction{
				SnapName:         "foo",
				Action:           "stop",
				Services:         []string{"svc1", "svc2"},
				ExplicitServices: []string{"svc1"},
				ScopeOptions: wrappers.ScopeOptions{
					Scope: "user",
					Users: []string{"baz"},
				},
			},
		},
	} {
		ts, err := servicestate.ServiceControlTs(st, appInfos, tc.instruction, nil)
		c.Assert(err, IsNil)
		s.checkServiceAction(c, ts, tc.expectedAction)
	}
}

func (s *snapServiceOptionsSuite) TestLogReader(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	si := snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	snp := &snap.Info{SideInfo: si}
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	appInfos := []*snap.AppInfo{
		{
			Snap:        snp,
			Name:        "svc1",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		{
			Snap:        snp,
			Name:        "svc2",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
	}

	restore := systemd.MockSystemdVersion(230, nil)
	defer restore()

	var jctlCalls int
	restore = systemd.MockJournalctl(func(svcs []string, n int, follow, namespaces bool) (rc io.ReadCloser, err error) {
		jctlCalls++
		c.Check(svcs, DeepEquals, []string{"snap.foo.svc1.service", "snap.foo.svc2.service"})
		c.Check(n, Equals, 100)
		c.Check(follow, Equals, false)
		c.Check(namespaces, Equals, false)
		return ioutil.NopCloser(strings.NewReader("")), nil
	})
	defer restore()

	_, err := servicestate.LogReader(appInfos, 100, false)
	c.Assert(err, IsNil)
	c.Check(jctlCalls, Equals, 1)
}

func (s *snapServiceOptionsSuite) TestLogReaderFailsWithNonServices(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	si := snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	snp := &snap.Info{SideInfo: si}
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	appInfos := []*snap.AppInfo{
		{
			Snap:        snp,
			Name:        "svc1",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		// Introduce a non-service to make sure we fail on this
		{
			Snap: snp,
			Name: "app1",
		},
	}

	_, err := servicestate.LogReader(appInfos, 100, false)
	c.Assert(err.Error(), Equals, `cannot read logs for app "app1": not a service`)
}

func (s *snapServiceOptionsSuite) TestLogReaderNamespaces(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	si := snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	snp := &snap.Info{SideInfo: si}
	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	appInfos := []*snap.AppInfo{
		{
			Snap:        snp,
			Name:        "svc1",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
		{
			Snap:        snp,
			Name:        "svc2",
			Daemon:      "simple",
			DaemonScope: snap.UserDaemon,
		},
	}

	var jctlCalls int

	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()
	restore = systemd.MockJournalctl(func(svcs []string, n int, follow, namespaces bool) (rc io.ReadCloser, err error) {
		jctlCalls++
		c.Check(svcs, DeepEquals, []string{"snap.foo.svc1.service", "snap.foo.svc2.service"})
		c.Check(n, Equals, 100)
		c.Check(follow, Equals, false)
		c.Check(namespaces, Equals, true)
		return ioutil.NopCloser(strings.NewReader("")), nil
	})
	defer restore()

	_, err := servicestate.LogReader(appInfos, 100, false)
	c.Assert(err, IsNil)
	c.Check(jctlCalls, Equals, 1)
}

// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
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
		c.Assert(args[0], Equals, "show")
		unit := args[2]
		activeState, unitState := "active", "enabled"
		if disabled {
			activeState = "inactive"
			unitState = "disabled"
		}
		if strings.HasSuffix(unit, ".timer") || strings.HasSuffix(unit, ".socket") {
			return []byte(fmt.Sprintf(`Id=%s
ActiveState=%s
UnitFileState=%s
`, args[2], activeState, unitState)), nil
		} else {
			return []byte(fmt.Sprintf(`Id=%s
Type=simple
ActiveState=%s
UnitFileState=%s
`, args[2], activeState, unitState)), nil
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

		// service with D-Bus activation
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
		c.Check(app.Enabled, Equals, enabled)
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
		snapApp.Timer = &snap.TimerInfo{
			App:   snapApp,
			Timer: "10:00",
		}

		err = sd.DecorateWithStatus(app, snapApp)
		c.Assert(err, IsNil)
		c.Check(app.Active, Equals, false)
		c.Check(app.Enabled, Equals, false)
		c.Check(app.Activators, HasLen, 0)
	}
}

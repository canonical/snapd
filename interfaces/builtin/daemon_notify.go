// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package builtin

import (
	"fmt"
	"os"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
)

const daemonNotifySummary = `allows sending daemon status changes to service manager`

const daemonNotifyBaseDeclarationSlots = `
  daemon-notify:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const daemonNotifyConnectedPlugAppArmorTemplate = `
# Allow sending notification messages to systemd through the notify socket
{{notify-socket-rule}},

# Allow using systemd-notify in shell scripts.
/{,usr/}bin/systemd-notify ixr,
`

type daemoNotifyInterface struct {
	commonInterface
}

var osGetenv = os.Getenv

func (iface *daemoNotifyInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// If the system has defined it, use NOTIFY_SOCKET from the environment. Note
	// this is safe because it is examined on snapd start and snaps cannot manipulate
	// the environment of snapd.
	notifySocket := osGetenv("NOTIFY_SOCKET")
	if notifySocket == "" {
		notifySocket = "/run/systemd/notify"
	}
	if !strings.HasPrefix(notifySocket, "/") && !strings.HasPrefix(notifySocket, "@") {
		// must be an absolute path or an abstract socket path
		return fmt.Errorf("cannot use %q as notify socket path: not absolute", notifySocket)
	}
	mylog.Check(apparmor_sandbox.ValidateNoAppArmorRegexp(notifySocket))

	var rule string

	switch {
	case strings.HasPrefix(notifySocket, "/"):
		rule = fmt.Sprintf(`"%s" w`, notifySocket)
	case strings.HasPrefix(notifySocket, "@/org/freedesktop/systemd1/notify/"):
		// special case for Ubuntu 14.04 where the manpage states that
		// /run/systemd/notify is used, but in fact the services get an
		// abstract socket path such as
		// @/org/freedesktop/systemd1/notify/13334051644891137417, the
		// last part changing with each reboot
		rule = `unix (connect, send) type=dgram peer=(label=unconfined,addr="@/org/freedesktop/systemd1/notify/[0-9]*")`
	case strings.HasPrefix(notifySocket, "@"):
		rule = fmt.Sprintf(`unix (connect, send) type=dgram peer=(label=unconfined,addr="%s")`, notifySocket)
	default:
		return fmt.Errorf("cannot use %q as notify socket path", notifySocket)
	}

	snippet := strings.Replace(daemonNotifyConnectedPlugAppArmorTemplate,
		"{{notify-socket-rule}}", rule, 1)
	spec.AddSnippet(snippet)
	return nil
}

func init() {
	registerIface(&daemoNotifyInterface{commonInterface: commonInterface{
		name:                 "daemon-notify",
		summary:              daemonNotifySummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: daemonNotifyBaseDeclarationSlots,
	}})
}

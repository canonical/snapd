// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
)

type installedSnapName string

func (s installedSnapName) Complete(match string) []flags.Completion {
	snaps := mylog.Check2(mkClient().List(nil, nil))

	ret := make([]flags.Completion, 0, len(snaps))
	for _, snap := range snaps {
		if strings.HasPrefix(snap.Name, match) {
			ret = append(ret, flags.Completion{Item: snap.Name})
		}
	}

	return ret
}

func installedSnapNames(snaps []installedSnapName) []string {
	names := make([]string, len(snaps))
	for i, name := range snaps {
		names[i] = string(name)
	}

	return names
}

func completeFromSortedFile(filename, match string) ([]flags.Completion, error) {
	file := mylog.Check2(os.Open(filename))

	defer file.Close()

	var ret []flags.Completion

	// TODO: look into implementing binary search
	//       e.g. https://github.com/pts/pts-line-bisect/
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line < match {
			continue
		}
		if !strings.HasPrefix(line, match) {
			break
		}
		ret = append(ret, flags.Completion{Item: line})
		if len(ret) > 10000 {
			// too many matches; slow machines could take too long to process this
			// e.g. the bbb takes ~1s to process ~2M entries (i.e. to reach the
			// point of asking the user if they actually want to see that many
			// results). 10k ought to be enough for anybody.
			break
		}
	}

	return ret, nil
}

type remoteSnapName string

func (s remoteSnapName) Complete(match string) []flags.Completion {
	if ret := mylog.Check2(completeFromSortedFile(dirs.SnapNamesFile, match)); err == nil {
		return ret
	}

	if len(match) < 3 {
		return nil
	}
	snaps, _ := mylog.Check3(mkClient().Find(&client.FindOptions{
		Query:  match,
		Prefix: true,
	}))

	ret := make([]flags.Completion, len(snaps))
	for i, snap := range snaps {
		ret[i] = flags.Completion{Item: snap.Name}
	}
	return ret
}

func remoteSnapNames(snaps []remoteSnapName) []string {
	names := make([]string, len(snaps))
	for i, name := range snaps {
		names[i] = string(name)
	}

	return names
}

type anySnapName string

func (s anySnapName) Complete(match string) []flags.Completion {
	res := installedSnapName(s).Complete(match)
	seen := make(map[string]bool)
	for _, x := range res {
		seen[x.Item] = true
	}

	for _, x := range remoteSnapName(s).Complete(match) {
		if !seen[x.Item] {
			res = append(res, x)
		}
	}

	return res
}

type changeID string

func (s changeID) Complete(match string) []flags.Completion {
	changes := mylog.Check2(mkClient().Changes(&client.ChangesOptions{Selector: client.ChangesAll}))

	ret := make([]flags.Completion, 0, len(changes))
	for _, change := range changes {
		if strings.HasPrefix(change.ID, match) {
			ret = append(ret, flags.Completion{Item: change.ID})
		}
	}

	return ret
}

type assertTypeName string

func (n assertTypeName) Complete(match string) []flags.Completion {
	cli := mkClient()
	names := mylog.Check2(cli.AssertionTypes())

	ret := make([]flags.Completion, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, match) {
			ret = append(ret, flags.Completion{Item: name})
		}
	}

	return ret
}

type keyName string

func (s keyName) Complete(match string) []flags.Completion {
	keypairManager := mylog.Check2(signtool.GetKeypairManager())

	keys := mylog.Check2(keypairManager.List())

	var res []flags.Completion
	for _, k := range keys {
		if strings.HasPrefix(k.Name, match) {
			res = append(res, flags.Completion{Item: k.Name})
		}
	}
	return res
}

type disconnectSlotOrPlugSpec struct {
	SnapAndNameStrict
}

func (dps disconnectSlotOrPlugSpec) Complete(match string) []flags.Completion {
	spec := &interfaceSpec{
		SnapAndName:  dps.SnapAndName,
		slots:        true,
		plugs:        true,
		connected:    true,
		disconnected: false,
	}
	return spec.Complete(match)
}

type disconnectSlotSpec struct {
	SnapAndNameStrict
}

// TODO: look at what the previous arg is, and filter accordingly
func (dss disconnectSlotSpec) Complete(match string) []flags.Completion {
	spec := &interfaceSpec{
		SnapAndName:  dss.SnapAndName,
		slots:        true,
		plugs:        false,
		connected:    true,
		disconnected: false,
	}
	return spec.Complete(match)
}

type connectPlugSpec struct {
	SnapAndName
}

func (cps connectPlugSpec) Complete(match string) []flags.Completion {
	spec := &interfaceSpec{
		SnapAndName:  cps.SnapAndName,
		slots:        false,
		plugs:        true,
		connected:    false,
		disconnected: true,
	}
	return spec.Complete(match)
}

type connectSlotSpec struct {
	SnapAndName
}

// TODO: look at what the previous arg is, and filter accordingly
func (css connectSlotSpec) Complete(match string) []flags.Completion {
	spec := &interfaceSpec{
		SnapAndName:  css.SnapAndName,
		slots:        true,
		plugs:        false,
		connected:    false,
		disconnected: true,
	}
	return spec.Complete(match)
}

type interfacesSlotOrPlugSpec struct {
	SnapAndName
}

func (is interfacesSlotOrPlugSpec) Complete(match string) []flags.Completion {
	spec := &interfaceSpec{
		SnapAndName:  is.SnapAndName,
		slots:        true,
		plugs:        true,
		connected:    true,
		disconnected: true,
	}
	return spec.Complete(match)
}

type interfaceSpec struct {
	SnapAndName
	slots        bool
	plugs        bool
	connected    bool
	disconnected bool
}

func (spec *interfaceSpec) connFilter(numConns int) bool {
	if spec.connected && numConns > 0 {
		return true
	}
	if spec.disconnected && numConns == 0 {
		return true
	}

	return false
}

func (spec *interfaceSpec) Complete(match string) []flags.Completion {
	// Parse what the user typed so far, it can be either
	// nothing (""), a "snap", a "snap:" or a "snap:name".
	parts := strings.SplitN(match, ":", 2)

	// Ask snapd about available interfaces.
	opts := client.ConnectionOptions{
		All: true,
	}
	ifaces := mylog.Check2(mkClient().Connections(&opts))

	snaps := make(map[string]bool)

	var ret []flags.Completion

	var prefix string
	if len(parts) == 2 {
		// The user typed the colon, means they know the snap they want;
		// go with that.
		prefix = parts[1]
		snaps[parts[0]] = true
	} else {
		// The user is about to or has started typing a snap name but didn't
		// reach the colon yet. Offer plugs for snaps with names that start
		// like that.
		snapPrefix := parts[0]
		if spec.plugs {
			for _, plug := range ifaces.Plugs {
				if strings.HasPrefix(plug.Snap, snapPrefix) && spec.connFilter(len(plug.Connections)) {
					snaps[plug.Snap] = true
				}
			}
		}
		if spec.slots {
			for _, slot := range ifaces.Slots {
				if strings.HasPrefix(slot.Snap, snapPrefix) && spec.connFilter(len(slot.Connections)) {
					snaps[slot.Snap] = true
				}
			}
		}
	}

	if len(snaps) == 1 {
		for snapName := range snaps {
			actualName := snapName
			if spec.plugs {
				if spec.connected && snapName == "" {
					actualName = "core"
				}
				for _, plug := range ifaces.Plugs {
					if plug.Snap == actualName && strings.HasPrefix(plug.Name, prefix) && spec.connFilter(len(plug.Connections)) {
						// TODO: in the future annotate plugs that can take
						// multiple connection sensibly and don't skip those even
						// if they have connections already.
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:%s", snapName, plug.Name), Description: "plug"})
					}
				}
			}
			if spec.slots {
				if actualName == "" {
					actualName = "core"
				}
				for _, slot := range ifaces.Slots {
					if slot.Snap == actualName && strings.HasPrefix(slot.Name, prefix) && spec.connFilter(len(slot.Connections)) {
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:%s", snapName, slot.Name), Description: "slot"})
					}
				}
			}
		}
	} else {
	snaps:
		for snapName := range snaps {
			if spec.plugs {
				for _, plug := range ifaces.Plugs {
					if plug.Snap == snapName && spec.connFilter(len(plug.Connections)) {
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:", snapName)})
						continue snaps
					}
				}
			}
			if spec.slots {
				for _, slot := range ifaces.Slots {
					if slot.Snap == snapName && spec.connFilter(len(slot.Connections)) {
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:", snapName)})
						continue snaps
					}
				}
			}
		}
	}

	return ret
}

type interfaceName string

func (s interfaceName) Complete(match string) []flags.Completion {
	ifaces := mylog.Check2(mkClient().Interfaces(nil))

	ret := make([]flags.Completion, 0, len(ifaces))
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, match) {
			ret = append(ret, flags.Completion{Item: iface.Name, Description: iface.Summary})
		}
	}

	return ret
}

type appName string

func (s appName) Complete(match string) []flags.Completion {
	cli := mkClient()
	apps := mylog.Check2(cli.Apps(nil, client.AppOptions{}))

	var ret []flags.Completion
	for _, app := range apps {
		if app.IsService() {
			continue
		}
		name := snap.JoinSnapApp(app.Snap, app.Name)
		if !strings.HasPrefix(name, match) {
			continue
		}
		ret = append(ret, flags.Completion{Item: name})
	}

	return ret
}

type serviceName string

func serviceNames(services []serviceName) []string {
	names := make([]string, len(services))
	for i, name := range services {
		names[i] = string(name)
	}

	return names
}

func (s serviceName) Complete(match string) []flags.Completion {
	cli := mkClient()
	apps := mylog.Check2(cli.Apps(nil, client.AppOptions{Service: true}))

	snaps := map[string]int{}
	var ret []flags.Completion
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		name := snap.JoinSnapApp(app.Snap, app.Name)
		if !strings.HasPrefix(name, match) {
			continue
		}
		ret = append(ret, flags.Completion{Item: name})
		if len(match) <= len(app.Snap) {
			snaps[app.Snap]++
		}
	}
	for snap, n := range snaps {
		if n > 1 {
			ret = append(ret, flags.Completion{Item: snap})
		}
	}

	return ret
}

type aliasOrSnap string

func (s aliasOrSnap) Complete(match string) []flags.Completion {
	aliases := mylog.Check2(mkClient().Aliases())

	var ret []flags.Completion
	for snap, aliases := range aliases {
		if strings.HasPrefix(snap, match) {
			ret = append(ret, flags.Completion{Item: snap})
		}
		for alias, status := range aliases {
			if status.Status == "disabled" {
				continue
			}
			if strings.HasPrefix(alias, match) {
				ret = append(ret, flags.Completion{Item: alias})
			}
		}
	}
	return ret
}

type snapshotID string

func (snapshotID) Complete(match string) []flags.Completion {
	shots := mylog.Check2(mkClient().SnapshotSets(0, nil))

	var ret []flags.Completion
	for _, sg := range shots {
		sid := strconv.FormatUint(sg.ID, 10)
		if strings.HasPrefix(sid, match) {
			ret = append(ret, flags.Completion{Item: sid})
		}
	}

	return ret
}

func (s snapshotID) ToUint() (uint64, error) {
	setID := mylog.Check2(strconv.ParseUint((string)(s), 10, 64))

	return setID, nil
}

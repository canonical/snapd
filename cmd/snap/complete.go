package main

import (
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
)

type installedSnapName string

func (s installedSnapName) Complete(match string) []flags.Completion {
	cli := Client()
	snaps, err := cli.List(nil, nil)
	if err != nil {
		return nil
	}

	ret := make([]flags.Completion, 0, len(snaps))
	for _, snap := range snaps {
		if strings.HasPrefix(snap.Name, match) {
			ret = append(ret, flags.Completion{Item: snap.Name})
		}
	}

	return ret
}

type remoteSnapName string

func (s remoteSnapName) Complete(match string) []flags.Completion {
	if len(match) < 3 {
		return nil
	}
	cli := Client()
	snaps, _, err := cli.Find(&client.FindOptions{
		Prefix: true,
		Query:  match,
	})
	if err != nil {
		return nil
	}
	ret := make([]flags.Completion, len(snaps))
	for i, snap := range snaps {
		ret[i] = flags.Completion{Item: snap.Name}
	}
	return ret
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
	cli := Client()
	changes, err := cli.Changes(&client.ChangesOptions{Selector: client.ChangesAll})
	if err != nil {
		return nil
	}

	ret := make([]flags.Completion, 0, len(changes))
	for _, change := range changes {
		if strings.HasPrefix(change.ID, match) {
			ret = append(ret, flags.Completion{Item: change.ID})
		}
	}

	return ret
}

type keyName string

func (s keyName) Complete(match string) []flags.Completion {
	var res []flags.Completion
	asserts.NewGPGKeypairManager().Walk(func(_ asserts.PrivateKey, _ string, uid string) error {
		if strings.HasPrefix(uid, match) {
			res = append(res, flags.Completion{Item: uid})
		}
		return nil
	})
	return res
}

type disconnectSlotOrPlugSpec struct {
	SnapAndName
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
	SnapAndName
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
	cli := Client()
	ifaces, err := cli.Interfaces()
	if err != nil {
		return nil
	}

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

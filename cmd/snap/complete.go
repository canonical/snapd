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

type connectPlugSpec struct {
	SnapAndName
}

func (cps connectPlugSpec) Complete(match string) []flags.Completion {
	match = strings.Trim(match, "\"'")

	// Parse what the user typed so far, it can be either
	// nothing (""), a "snap", a "snap:" or a "snap:name".
	parts := strings.Split(match, ":")

	// Ask snapd about available interfaces.
	cli := Client()
	ifaces, err := cli.Interfaces()
	if err != nil {
		return nil
	}
	var ret []flags.Completion
	switch len(parts) {
	case 0:
		// The user didn't input anything yet. Let's start by offering
		// suggestion containing snap names that have at least one plug.
		snaps := make(map[string]bool)
		for _, plug := range ifaces.Plugs {
			snaps[plug.Snap] = true
		}
		for snapName := range snaps {
			ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:", snapName)})
		}
		break
	case 1:
		// The user started typing a snap name but didn't reach the colon yet.
		// Let's help by offering suggestions containing snap names that start
		// with the given text and have at least one plug.
		partialSnapName := parts[0]
		snaps := make(map[string]bool)
		for _, plug := range ifaces.Plugs {
			if partialSnapName == "" || strings.HasPrefix(plug.Snap, partialSnapName) {
				snaps[plug.Snap] = true
			}
		}
		for snapName := range snaps {
			ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:", snapName)})
		}
		break
	case 2:
		// The user typed the snap name and the colon and is starting to narrow
		// down the input to specify plug name. Let's help by offering full
		// suggestions containing the suggestions of possible plug names that
		// start with the given text (the plug part of if).
		snapName := parts[0]
		partialPlugName := parts[1]
		plugs := make(map[string]bool)
		for _, plug := range ifaces.Plugs {
			if plug.Snap == snapName && (partialPlugName == "" || strings.HasPrefix(plug.Name, partialPlugName)) {
				plugs[plug.Name] = true
			}
		}
		for plugName := range plugs {
			ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:%s", snapName, plugName)})
		}
		break
	}
	return ret
}

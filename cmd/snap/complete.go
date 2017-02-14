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

	var snapPrefix, plugPrefix string
	if len(parts) == 2 {
		// The user typed the colon, means they know the snap they want;
		// go with that.
		plugPrefix = parts[1]
		snaps[parts[0]] = true
	} else {
		// The user is about to or has started typing a snap name but didn't
		// reach the colon yet. Offer plugs for snaps with names that start
		// like that.
		snapPrefix = parts[0]
		for _, plug := range ifaces.Plugs {
			if strings.HasPrefix(plug.Snap, snapPrefix) {
				snaps[plug.Snap] = true
			}
		}
	}

	if len(snaps) == 1 {
		for snapName := range snaps {
			for _, plug := range ifaces.Plugs {
				if plug.Snap == snapName && strings.HasPrefix(plug.Name, plugPrefix) {
					// TODO: in the future annotate plugs that can take
					// multiple connection sensibly and don't skip those even
					// if they have connections already.
					if len(plug.Connections) == 0 {
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:%s", plug.Snap, plug.Name)})
					}
				}
			}
		}
	} else {
		for snapName := range snaps {
			for _, plug := range ifaces.Plugs {
				if plug.Snap == snapName {
					if len(plug.Connections) == 0 {
						// TODO: in the future annotate plugs that can take
						// multiple connection sensibly and don't skip those
						// even if they have connections already.
						ret = append(ret, flags.Completion{Item: fmt.Sprintf("%s:", snapName)})
					}
				}
			}
		}
	}

	return ret
}

package main

import (
	"os"
	"path/filepath"
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

type snapDir string

func (s snapDir) Complete(match string) []flags.Completion {
	dir, prefix := filepath.Split(match)
	if dir == "" {
		dir = "."
	}
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	var res []flags.Completion
	for {
		fis, _ := d.Readdir(100)
		if len(fis) == 0 {
			break
		}
		for _, fi := range fis {
			name := fi.Name()
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			if fi.IsDir() {
				res = append(res, flags.Completion{Item: filepath.Join(dir, name) + "/"})
			}
		}
	}

	return res
}

type snapFile string

func (s snapFile) Complete(match string) []flags.Completion {
	dir, prefix := filepath.Split(match)
	if dir == "" {
		dir = "."
	}
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	var res []flags.Completion
	for {
		fis, _ := d.Readdir(100)
		if len(fis) == 0 {
			break
		}
		for _, fi := range fis {
			name := fi.Name()
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			if fi.IsDir() {
				res = append(res, flags.Completion{Item: filepath.Join(dir, name) + "/"})
			} else if strings.HasSuffix(name, ".snap") {
				res = append(res, flags.Completion{Item: filepath.Join(dir, name)})
			}
		}
	}

	return res
}

type snapFileOrRemoteSnapName string

func (s snapFileOrRemoteSnapName) Complete(match string) []flags.Completion {
	if strings.HasPrefix(match, ".") || strings.HasPrefix(match, "/") {
		return snapFile(s).Complete(match)
	}
	return remoteSnapName(s).Complete(match)
}

type anySnap string

func (s anySnap) Complete(match string) []flags.Completion {
	if strings.HasPrefix(match, ".") || strings.HasPrefix(match, "/") {
		return snapFile(s).Complete(match)
	}

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

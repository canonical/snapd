// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/interfaces"
)

type cmdSeeding struct {
	clientMixin
}

func init() {
	cmd := addDebugCommand("seeding",
		"(internal) obtain seeding and preseeding details",
		"(internal) obtain seeding and preseeding details",
		func() flags.Commander {
			return &cmdSeeding{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdSeeding) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		Seeded               bool        `json:"seeded,omitempty"`
		Preseeded            bool        `json:"preseeded,omitempty"`
		PreseedStartTime     time.Time   `json:"preseed-start-time,omitempty"`
		PreseedTime          time.Time   `json:"preseed-time,omitempty"`
		SeedStartTime        time.Time   `json:"seed-start-time,omitempty"`
		SeedRestartTime      time.Time   `json:"seed-restart-time,omitempty"`
		SeedTime             time.Time   `json:"seed-time,omitempty"`
		PreseedSystemKey     interface{} `json:"preseed-system-key,omitempty"`
		SeedRestartSystemKey interface{} `json:"seed-restart-system-key,omitempty"`
	}
	if err := x.client.DebugGet("seeding", &resp, nil); err != nil {
		return err
	}

	w := tabWriter()

	// show seeded and preseeded keys
	fmt.Fprintf(w, "seeded:\t%v\n", resp.Seeded)
	fmt.Fprintf(w, "preseeded:\t%v\n", resp.Preseeded)

	// calculate the time spent preseeding (if preseeded) and seeding
	// for the preseeded case, we use the seed-restart-time as the start time
	// to show how long we spent only after booting the preseeded image
	var seedDuration time.Duration
	if resp.Preseeded {
		preseedDuration := resp.PreseedTime.Sub(resp.PreseedStartTime)
		fmt.Fprintf(w, "image-preseeding:\t%v\n", preseedDuration)
		seedDuration = resp.SeedTime.Sub(resp.SeedRestartTime)
	} else {
		seedDuration = resp.SeedTime.Sub(resp.SeedStartTime)
	}
	fmt.Fprintf(w, "seed-completion:\t%v\n", seedDuration)

	// we flush the tabwriter now because if we have more output, it will be
	// the system keys, which are JSON and thus will never display cleanly in
	// line with the other keys we did above
	w.Flush()

	// only compare system-keys if preseeded and the system-keys exist
	// they might not exist if this command is used on a system that was
	// preseeded with an older version of snapd, i.e. while this feature is
	// being rolled out, we may be preseeding images via old snapd deb, but with
	// new snapd snap
	if resp.Preseeded && resp.SeedRestartSystemKey != nil && resp.PreseedSystemKey != nil {
		// only show them if they don't match

		// we have to play a bit of a dance here, first marshalling the
		// interface{} we got over JSON, which will basically just be a
		// map[string]interface{} into bytes, then having the interfaces pkg
		// decode those bytes into a proper interfaces.systemKey (which is not
		// exported) and return that back to us as an interface{} again, which
		// we can then use to compare the two system keys, and then finally
		// display the originally marshalled json
		seedRestartSkJSON, err := json.MarshalIndent(resp.SeedRestartSystemKey, "", "  ")
		if err != nil {
			return err
		}

		seedSk, err := interfaces.UnmarshalJSONSystemKey(bytes.NewReader(seedRestartSkJSON))
		if err != nil {
			return err
		}

		preseedSkJSON, err := json.MarshalIndent(resp.PreseedSystemKey, "", "  ")
		if err != nil {
			return err
		}

		preseedSk, err := interfaces.UnmarshalJSONSystemKey(bytes.NewReader(preseedSkJSON))
		if err != nil {
			return err
		}

		match, err := interfaces.SystemKeysMatch(preseedSk, seedSk)
		if err != nil {
			return err
		}
		if !match {
			// mismatch, display the different keys
			fmt.Fprintf(Stdout, "preseed-system-key: %s\n", string(preseedSkJSON))
			fmt.Fprintf(Stdout, "seed-restart-system-key: %s\n", string(seedRestartSkJSON))
		}
	}

	return nil
}

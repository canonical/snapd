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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/interfaces"
)

type cmdSeeding struct {
	clientMixin
	unicodeMixin
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
	esc := x.getEscapes()

	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		Seeded           bool       `json:"seeded,omitempty"`
		Preseeded        bool       `json:"preseeded,omitempty"`
		PreseedStartTime *time.Time `json:"preseed-start-time,omitempty"`
		PreseedTime      *time.Time `json:"preseed-time,omitempty"`
		SeedStartTime    *time.Time `json:"seed-start-time,omitempty"`
		SeedRestartTime  *time.Time `json:"seed-restart-time,omitempty"`
		SeedTime         *time.Time `json:"seed-time,omitempty"`
		// use json.RawMessage to delay unmarshal'ing to the interfaces pkg
		PreseedSystemKey     *json.RawMessage `json:"preseed-system-key,omitempty"`
		SeedRestartSystemKey *json.RawMessage `json:"seed-restart-system-key,omitempty"`

		SeedError string `json:"seed-error,omitempty"`
	}
	mylog.Check(x.client.DebugGet("seeding", &resp, nil))

	w := tabWriter()

	// show seeded and preseeded keys
	fmt.Fprintf(w, "seeded:\t%v\n", resp.Seeded)
	if resp.SeedError != "" {
		// print seed-error
		termWidth, _ := termSize()
		termWidth -= 3
		if termWidth > 100 {
			// any wider than this and it gets hard to read
			termWidth = 100
		}
		fmt.Fprintln(w, "seed-error: |")
		// XXX: reuse/abuse
		printDescr(w, resp.SeedError, termWidth)
	}

	fmt.Fprintf(w, "preseeded:\t%v\n", resp.Preseeded)

	// calculate the time spent preseeding (if preseeded) and seeding
	// for the preseeded case, we use the seed-restart-time as the start time
	// to show how long we spent only after booting the preseeded image

	// if we are missing time values, we will default to showing "-" for the
	// duration
	seedDuration := esc.dash
	if resp.Preseeded {
		if resp.PreseedTime != nil && resp.PreseedStartTime != nil {
			preseedDuration := resp.PreseedTime.Sub(*resp.PreseedStartTime).Round(time.Millisecond)
			fmt.Fprintf(w, "image-preseeding:\t%v\n", preseedDuration)
		} else {
			fmt.Fprintf(w, "image-preseeding:\t%s\n", esc.dash)
		}

		if resp.SeedTime != nil && resp.SeedRestartTime != nil {
			seedDuration = fmt.Sprintf("%v", resp.SeedTime.Sub(*resp.SeedRestartTime).Round(time.Millisecond))
		}
	} else if resp.SeedTime != nil && resp.SeedStartTime != nil {
		seedDuration = fmt.Sprintf("%v", resp.SeedTime.Sub(*resp.SeedStartTime).Round(time.Millisecond))
	}
	fmt.Fprintf(w, "seed-completion:\t%s\n", seedDuration)

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
		// only show them if they don't match, so first unmarshal them so we can
		// properly compare them

		// we use raw json messages here so that the interfaces pkg can do the
		// real unmarshalling to a real systemKey interface{} that can be
		// compared with SystemKeysMatch, if we had instead unmarshalled here,
		// we would have to remarshal the map[string]interface{} we got above
		// and then pass those bytes back to the interfaces pkg which is awkward
		seedSk := mylog.Check2(interfaces.UnmarshalJSONSystemKey(bytes.NewReader(*resp.SeedRestartSystemKey)))

		preseedSk := mylog.Check2(interfaces.UnmarshalJSONSystemKey(bytes.NewReader(*resp.PreseedSystemKey)))

		match := mylog.Check2(interfaces.SystemKeysMatch(preseedSk, seedSk))

		if !match {
			// mismatch, display the different keys
			var preseedSkJSON, seedRestartSkJSON bytes.Buffer
			json.Indent(&preseedSkJSON, *resp.PreseedSystemKey, "", "  ")
			fmt.Fprintf(Stdout, "preseed-system-key: ")
			preseedSkJSON.WriteTo(Stdout)
			fmt.Fprintln(Stdout, "")

			json.Indent(&seedRestartSkJSON, *resp.SeedRestartSystemKey, "", "  ")
			fmt.Fprintf(Stdout, "seed-restart-system-key: ")
			seedRestartSkJSON.WriteTo(Stdout)
			fmt.Fprintln(Stdout, "")
		}
	}

	return nil
}

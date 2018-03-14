// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdAliases struct {
	Positionals struct {
		Snap installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"true"`
}

var shortAliasesHelp = i18n.G("List aliases in the system")
var longAliasesHelp = i18n.G(`
The aliases command lists all aliases available in the system and their status.

$ snap aliases <snap>

Lists only the aliases defined by the specified snap.

An alias noted as undefined means it was explicitly enabled or disabled but is
not defined in the current revision of the snap, possibly temporarily (e.g.
because of a revert). This can cleared with 'snap alias --reset'.
`)

func init() {
	addCommand("aliases", shortAliasesHelp, longAliasesHelp, func() flags.Commander {
		return &cmdAliases{}
	}, nil, nil)
}

type aliasInfo struct {
	Snap    string
	Command string
	Alias   string
	Status  string
	Auto    string
}

type aliasInfos []*aliasInfo

func (infos aliasInfos) Len() int      { return len(infos) }
func (infos aliasInfos) Swap(i, j int) { infos[i], infos[j] = infos[j], infos[i] }
func (infos aliasInfos) Less(i, j int) bool {
	if infos[i].Snap < infos[j].Snap {
		return true
	}
	if infos[i].Snap == infos[j].Snap {
		if infos[i].Command < infos[j].Command {
			return true
		}
		if infos[i].Command == infos[j].Command {
			if infos[i].Alias < infos[j].Alias {
				return true
			}
		}
	}
	return false
}

func (x *cmdAliases) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	allStatuses, err := Client().Aliases()
	if err != nil {
		return err
	}

	var infos aliasInfos
	filterSnap := string(x.Positionals.Snap)
	if filterSnap != "" {
		allStatuses = map[string]map[string]client.AliasStatus{
			filterSnap: allStatuses[filterSnap],
		}
	}
	for snapName, aliasStatuses := range allStatuses {
		for alias, aliasStatus := range aliasStatuses {
			infos = append(infos, &aliasInfo{
				Snap:    snapName,
				Command: aliasStatus.Command,
				Alias:   alias,
				Status:  aliasStatus.Status,
				Auto:    aliasStatus.Auto,
			})
		}
	}

	if len(infos) > 0 {
		w := tabWriter()
		fmt.Fprintln(w, i18n.G("Command\tAlias\tNotes"))
		defer w.Flush()

		sort.Sort(infos)

		for _, info := range infos {
			var notes []string
			if info.Status != "auto" {
				notes = append(notes, info.Status)
				if info.Status == "manual" && info.Auto != "" {
					notes = append(notes, "override")
				}
			}
			notesStr := strings.Join(notes, ",")
			if notesStr == "" {
				notesStr = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", info.Command, info.Alias, notesStr)
		}
	} else {
		if filterSnap != "" {
			fmt.Fprintf(Stderr, i18n.G("No aliases are currently defined for snap %q.\n"), filterSnap)
		} else {
			fmt.Fprintln(Stderr, i18n.G("No aliases are currently defined."))
		}
		fmt.Fprintln(Stderr, i18n.G("\nUse 'snap help alias' to learn how to create aliases manually."))
	}
	return nil
}

// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"text/tabwriter"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

type reportProblemCmd struct {
	clientMixin

	Positional struct {
		Snaps []anySnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortReportProblemHelp = i18n.G("Report a problem with a snap to its publisher")
var longReportProblemHelp = i18n.G(`
The report-problem command helps with reporting a problem to the snap's publisher.
`)

func init() {
	addCommand("report-problem",
		shortReportProblemHelp,
		longReportProblemHelp,
		func() flags.Commander {
			return &reportProblemCmd{}
		}, nil, nil)
}

func skipGenericLink(category, link string) bool {
	if category == "website" && link == "https://snapcraft.io" {
		//  skip a link poiting to the main store page
		return true
	}
	return false
}

func collectLinks(linksBag map[string]map[string]bool, fromSnap *client.Snap) {
	for category, links := range fromSnap.Links {
		m := linksBag[category]

		if m == nil {
			m = make(map[string]bool, len(links))
		}

		for _, link := range links {
			logger.Debugf("category %v link %v", category, link)
			if skipGenericLink(category, link) {
				continue
			}
			m[link] = true
		}

		if len(m) > 0 {
			linksBag[category] = m
		}
	}
}

func addCategoryLinks(linksBag map[string]map[string]bool, category, link string) {
	if link == "" {
		return
	}

	if skipGenericLink(category, link) {
		return
	}

	c := linksBag[category]
	if c == nil {
		c = make(map[string]bool)
	}

	c[link] = true
	linksBag[category] = c
}

func (x *reportProblemCmd) Execute([]string) error {
	for i, snapName := range x.Positional.Snaps {
		snapName := string(snapName)
		if i > 0 {
			fmt.Fprintln(Stdout, "---")
		}

		if snapName == "system" {
			continue
		}

		remoteSnap, _, _ := x.client.FindOne(snap.InstanceSnap(snapName))
		localSnap, _, _ := x.client.Snap(snapName)

		allLinks := map[string]map[string]bool{}
		if remoteSnap != nil {
			collectLinks(allLinks, remoteSnap)
			addCategoryLinks(allLinks, "contact", remoteSnap.Contact)
			addCategoryLinks(allLinks, "website", remoteSnap.Website)
		}
		if localSnap != nil {
			collectLinks(allLinks, localSnap)
			addCategoryLinks(allLinks, "contact", localSnap.Contact)
			addCategoryLinks(allLinks, "website", localSnap.Website)
		}

		if len(allLinks) > 0 {
			fmt.Fprintf(Stdout, i18n.G("Publisher of snap %q has listed the following points of contact:\n"), snapName)

			w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

			if c, ok := allLinks["contact"]; ok {
				fmt.Fprintf(w, i18n.G("\tContact:\n"))
				for k := range c {
					fmt.Fprintf(w, "\t\t%s\n", k)
				}
			}

			if c, ok := allLinks["issues"]; ok {
				fmt.Fprintf(w, i18n.G("\tIssue reporting:\n"))
				for k := range c {
					fmt.Fprintf(w, "\t\t%s\n", k)
				}
			}

			if c, ok := allLinks["website"]; ok {
				fmt.Fprintf(w, i18n.G("\tWebsite:\n"))
				for k := range c {
					fmt.Fprintf(w, "\t\t%s\n", k)
				}
			}

			if c, ok := allLinks["source"]; ok {
				fmt.Fprintf(w, i18n.G("\tSource code:\n"))
				for k := range c {
					fmt.Fprintf(w, "\t\t%s\n", k)
				}
			}
			w.Flush()

		} else {
			fmt.Fprintf(Stdout, i18n.G("Publisher of snap %q has not listed any points of contact"), snapName)
		}
	}

	return nil
}

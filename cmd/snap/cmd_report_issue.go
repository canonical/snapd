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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

type reportIssueCmd struct {
	clientMixin

	Positional struct {
		Snap anySnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortReportIssueHelp = i18n.G("Show contact information and optionally navigate to relevant issue tracker")
var longReportIssueHelp = i18n.G(`
The report-issue command helps with reporting a problem with a snap by listing
available contact information provided by the snap's publisher and optionally
opens the issue reporting link in user's web browser.
`)

func init() {
	addCommand("report-issue",
		shortReportIssueHelp,
		longReportIssueHelp,
		func() flags.Commander {
			return &reportIssueCmd{}
		}, nil, nil)
}

func skipGenericLink(category, link string) bool {
	// store page is not the snap's website
	return category == "website" && strings.Contains(link, "://snapcraft.io")
}

func collectLinks(linksBag map[string]map[string]bool, fromSnap *client.Snap) {
	for category, links := range fromSnap.Links {
		m := linksBag[category]

		if m == nil {
			m = make(map[string]bool, len(links))
		}

		for _, link := range links {
			logger.Debugf("category %v link %v", category, link)
			if link == "" {
				continue
			}
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

// TODO:GOVERSION: use maps.Keys()
func keys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func (x *reportIssueCmd) reportContacts(snapName string) (hadLinks bool, issueReportingLinks []string) {
	remoteSnap, _, remoteErr := x.client.FindOne(snap.InstanceSnap(snapName))
	localSnap, _, localErr := x.client.Snap(snapName)

	allLinks := map[string]map[string]bool{}

	if remoteErr != nil {
		logger.Debugf("remote snap err: %v\n", remoteErr)
	}
	if remoteSnap != nil {
		collectLinks(allLinks, remoteSnap)
		addCategoryLinks(allLinks, "contact", remoteSnap.Contact)
		addCategoryLinks(allLinks, "website", remoteSnap.Website)
	}

	if localErr != nil {
		logger.Debugf("local snap err: %v\n", localErr)
	}
	if localSnap != nil {
		collectLinks(allLinks, localSnap)
		addCategoryLinks(allLinks, "contact", localSnap.Contact)
		addCategoryLinks(allLinks, "website", localSnap.Website)
	}

	logger.Debugf("all links: %+v", allLinks)

	if len(allLinks) == 0 {
		fmt.Fprintf(Stdout, i18n.G("Publisher of snap %q has not listed any points of contact.\n"), snapName)
		return false, nil
	}

	fmt.Fprintf(Stdout, i18n.G("Publisher of snap %q has listed the following points of contact:\n"), snapName)

	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

	if c, ok := allLinks["contact"]; ok {
		c := keys(c)
		sort.Strings(c)
		fmt.Fprintf(w, i18n.G("\tContact:\n"))
		for _, k := range c {
			fmt.Fprintf(w, "\t\t%s\n", k)
		}
	}

	if c, ok := allLinks["issues"]; ok {
		c := keys(c)
		sort.Strings(c)

		issueReportingLinks = c

		fmt.Fprintf(w, i18n.G("\tIssue reporting:\n"))
		for _, k := range c {
			fmt.Fprintf(w, "\t\t%s\n", k)
		}
	}

	if c, ok := allLinks["website"]; ok {
		c := keys(c)
		sort.Strings(c)
		fmt.Fprintf(w, i18n.G("\tWebsite:\n"))
		for _, k := range c {
			fmt.Fprintf(w, "\t\t%s\n", k)
		}
	}

	for _, sourceName := range []string{"source", "source-code"} {
		if c, ok := allLinks[sourceName]; ok {
			c := keys(c)
			sort.Strings(c)
			fmt.Fprintf(w, i18n.G("\tSource code:\n"))
			for _, k := range c {
				fmt.Fprintf(w, "\t\t%s\n", k)
			}
			break
		}
	}
	w.Flush()

	return true, issueReportingLinks
}

func isDesktop() bool {
	return os.Getenv("DESKTOP_SESSION") != "" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func (x *reportIssueCmd) Execute([]string) error {
	snapName := string(x.Positional.Snap)

	if snapName == "system" {
		logger.Noticef("'system' is provided by snapd")
		snapName = "snapd"
	}

	hadLinks, issueReportingLinks := x.reportContacts(snapName)

	if hadLinks {
		fmt.Fprint(Stdout, i18n.G("\nUse one of the links listed above to report an issue with the snap.\n"))
	}

	if isStdinTTY && isDesktop() && len(issueReportingLinks) > 0 {
		xdgOpen, _ := exec.LookPath("xdg-open")
		if xdgOpen != "" {
			fmt.Fprint(Stdout, i18n.G("\nWould you like to open the first issue tracker link in browser? [Y/n] "))

			s, err := bufio.NewReader(Stdin).ReadString('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) {
					return err
				}
				return nil
			}

			switch s {
			case "y\n", "Y\n", "yes\n", "\n":
				return exec.Command(xdgOpen, issueReportingLinks[0]).Run()
			}
		}
	}
	return nil
}

// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"errors"
	"fmt"
	"go/doc"
	"os"
	"os/user"
	"strings"
	"text/tabwriter"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var errorPrefix = i18n.G("error: %v\n")

func termSize() (width, height int) {
	if f, ok := Stdout.(*os.File); ok {
		width, height, _ = terminal.GetSize(int(f.Fd()))
	}

	if width <= 0 {
		width = int(osutil.GetenvInt64("COLUMNS"))
	}

	if height <= 0 {
		height = int(osutil.GetenvInt64("LINES"))
	}

	if width < 40 {
		width = 80
	}

	if height < 15 {
		height = 25
	}

	return width, height
}

func fill(para string, indent int) string {
	width, _ := termSize()

	if width > 100 {
		width = 100
	}

	// some terminals aren't happy about writing in the last
	// column (they'll add line for you). We could check terminfo
	// for "sam" (semi_auto_right_margin), but that's a lot of
	// work just for this.
	width--

	var buf bytes.Buffer
	indentStr := strings.Repeat(" ", indent)
	doc.ToText(&buf, para, indentStr, indentStr, width-indent)

	return strings.TrimSpace(buf.String())
}

func errorToCmdMessage(snapName string, e error, opts *client.SnapOptions) (string, error) {
	// do this here instead of in the caller for more DRY
	err, ok := e.(*client.Error)
	if !ok {
		return "", e
	}

	// ensure the "real" error is available if we ask for it
	logger.Debugf("error: %s", err)

	// FIXME: using err.Message in user-facing messaging is not
	// l10n-friendly, and probably means we're missing ad-hoc messaging.
	isError := true
	usesSnapName := true
	var msg string
	switch err.Kind {
	case client.ErrorKindSnapNotFound:
		msg = i18n.G("snap %q not found")
		if snapName == "" {
			errValStr, ok := err.Value.(string)
			if ok && errValStr != "" {
				snapName = errValStr
			}
		}
	case client.ErrorKindChannelNotAvailable,
		client.ErrorKindArchitectureNotAvailable:
		values, ok := err.Value.(map[string]interface{})
		if ok {
			candName, _ := values["snap-name"].(string)
			if candName != "" {
				snapName = candName
			}
			action, _ := values["action"].(string)
			arch, _ := values["architecture"].(string)
			channel, _ := values["channel"].(string)
			releases, _ := values["releases"].([]interface{})
			if snapName != "" && action != "" && arch != "" && channel != "" && len(releases) != 0 {
				usesSnapName = false
				msg = snapRevisionNotAvailableMessage(err.Kind, snapName, action, arch, channel, releases)
				break
			}
		}
		fallthrough
	case client.ErrorKindRevisionNotAvailable:
		if snapName == "" {
			errValStr, ok := err.Value.(string)
			if ok && errValStr != "" {
				snapName = errValStr
			}
		}

		usesSnapName = false
		// TRANSLATORS: %[1]q and %[1]s refer to the same thing (a snap name).
		msg = fmt.Sprintf(i18n.G(`snap %[1]q not available as specified (see 'snap info %[1]s')`), snapName)

		if opts != nil {
			if opts.Revision != "" {
				// TRANSLATORS: %[1]q and %[1]s refer to the same thing (a snap name); %s is whatever the user used for --revision=
				msg = fmt.Sprintf(i18n.G(`snap %[1]q revision %s not available (see 'snap info %[1]s')`), snapName, opts.Revision)
			} else if opts.Channel != "" {
				// (note --revision overrides --channel)

				// TRANSLATORS: %[1]q and %[1]s refer to the same thing (a snap name); %q is whatever foo the user used for --channel=foo
				msg = fmt.Sprintf(i18n.G(`snap %[1]q not available on channel %q (see 'snap info %[1]s')`), snapName, opts.Channel)
			}
		}
	case client.ErrorKindSnapAlreadyInstalled:
		isError = false
		msg = i18n.G(`snap %q is already installed, see 'snap help refresh'`)
	case client.ErrorKindSnapNeedsDevMode:
		msg = i18n.G(`
The publisher of snap %q has indicated that they do not consider this revision
to be of production quality and that it is only meant for development or testing
at this point. As a consequence this snap will not refresh automatically and may
perform arbitrary system changes outside of the security sandbox snaps are
generally confined to, which may put your system at risk.

If you understand and want to proceed repeat the command including --devmode;
if instead you want to install the snap forcing it into strict confinement
repeat the command including --jailmode.`)
	case client.ErrorKindSnapNeedsClassic:
		msg = i18n.G(`
This revision of snap %q was published using classic confinement and thus may
perform arbitrary system changes outside of the security sandbox that snaps are
usually confined to, which may put your system at risk.

If you understand and want to proceed repeat the command including --classic.
`)
	case client.ErrorKindLoginRequired:
		usesSnapName = false
		u, _ := user.Current()
		if u != nil && u.Username == "root" {
			// TRANSLATORS: %s is an error message (e.g. “cannot yadda yadda: permission denied”)
			msg = fmt.Sprintf(i18n.G(`%s (see 'snap help login')`), err.Message)
		} else {
			// TRANSLATORS: %s is an error message (e.g. “cannot yadda yadda: permission denied”)
			msg = fmt.Sprintf(i18n.G(`%s (try with sudo)`), err.Message)
		}
	case client.ErrorKindSnapLocal:
		msg = i18n.G("local snap %q is unknown to the store, use --amend to proceed anyway")
	case client.ErrorKindNoUpdateAvailable:
		isError = false
		msg = i18n.G("snap %q has no updates available")
	case client.ErrorKindSnapNotInstalled:
		isError = false
		usesSnapName = false
		msg = err.Message
	case client.ErrorKindNetworkTimeout:
		isError = true
		usesSnapName = false
		msg = i18n.G("unable to contact snap store")
	case client.ErrorKindSystemRestart:
		isError = false
		usesSnapName = false
		msg = i18n.G("snapd is about to reboot the system")
	default:
		usesSnapName = false
		msg = err.Message
	}

	if usesSnapName {
		msg = fmt.Sprintf(msg, snapName)
	}
	// 3 is the %v\n, which will be present in any locale
	msg = fill(msg, len(errorPrefix)-3)
	if isError {
		return "", errors.New(msg)
	}

	return msg, nil
}

func snapRevisionNotAvailableMessage(kind, snapName, action, arch, channel string, releases []interface{}) string {
	// releases contains all available (arch x channel)
	// as reported by the store through the daemon
	req, err := snap.ParseChannel(channel, arch)
	if err != nil {
		// TRANSLATORS: %q is the invalid request channel, %s is the snap name
		msg := fmt.Sprintf(i18n.G("requested channel %q is not valid (see 'snap info %s' for valid ones)"), channel, snapName)
		return msg
	}
	avail := make([]*snap.Channel, 0, len(releases))
	for _, v := range releases {
		rel, _ := v.(map[string]interface{})
		relCh, _ := rel["channel"].(string)
		relArch, _ := rel["architecture"].(string)
		if relArch == "" {
			logger.Debugf("internal error: %q daemon error carries a release with invalid/empty architecture: %v", kind, v)
			continue
		}
		a, err := snap.ParseChannel(relCh, relArch)
		if err != nil {
			logger.Debugf("internal error: %q daemon error carries a release with invalid/empty channel (%v): %v", kind, err, v)
			continue
		}
		avail = append(avail, &a)
	}

	matches := map[string][]*snap.Channel{}
	for _, a := range avail {
		m := req.Match(a)
		matchRepr := m.String()
		if matchRepr != "" {
			matches[matchRepr] = append(matches[matchRepr], a)
		}
	}

	// no release is for this architecture
	if kind == client.ErrorKindArchitectureNotAvailable {
		// TODO: add "Get more information..." hints once snap info
		// support showing multiple/all archs

		// there are matching track+risk releases for other archs
		if hits := matches["track:risk"]; len(hits) != 0 {
			archs := strings.Join(archsForChannels(hits), ", ")
			// TRANSLATORS: %q is for the snap name, %v is the requested channel, first %s is the system architecture short name, second %s is a comma separated list of available arch short names
			msg := fmt.Sprintf(i18n.G("snap %q is not available on %v for this architecture (%s) but exists on other architectures (%s)."), snapName, req, arch, archs)
			return msg
		}

		// not even that, generic error
		archs := strings.Join(archsForChannels(avail), ", ")
		// TRANSLATORS: %q is for the snap name, first %s is the system architecture short name, second %s is a comma separated list of available arch short names
		msg := fmt.Sprintf(i18n.G("snap %q is not available on this architecture (%s) but exists on other architectures (%s)."), snapName, arch, archs)
		return msg
	}

	// a branch was requested
	if req.Branch != "" {
		// there are matching arch+track+risk, give main track info
		if len(matches["architecture:track:risk"]) != 0 {
			trackRisk := snap.Channel{Track: req.Track, Risk: req.Risk}
			trackRisk = trackRisk.Clean()

			// TRANSLATORS: %q is for the snap name, first %s is the full requested channel
			msg := fmt.Sprintf(i18n.G("requested a non-existing branch on %s for snap %q: %s"), trackRisk.Full(), snapName, req.Branch)
			return msg
		}

		msg := fmt.Sprintf(i18n.G("requested a non-existing branch for snap %q: %s"), snapName, req.Full())
		return msg
	}

	// TRANSLATORS: can optionally be concatenated after a blank line at the end of other error messages, together with the "Get more information ..." hint
	preRelWarn := i18n.G("Please be mindful pre-release channels may include features not completely tested or implemented.")
	// TRANSLATORS: can optionally be concatenated after a blank line at the end of other error messages, together with the "Get more information ..." hint
	trackWarn := i18n.G("Please be mindful that different tracks may include different features.")
	// TRANSLATORS: %s is for the snap name, will be concatenated after at the end of other error messages, possibly after a blank line
	moreInfoHint := fmt.Sprintf(i18n.G("Get more information with 'snap info %s'."), snapName)

	// there are matching arch+track releases => give hint and instructions
	// about pre-release channels
	if hits := matches["architecture:track"]; len(hits) != 0 {
		// TRANSLATORS: %q is for the snap name, %v is the requested channel
		msg := fmt.Sprintf(i18n.G("snap %q is not available on %v but is available to install on the following channels:\n"), snapName, req)
		msg += installTable(snapName, action, hits, false)
		msg += "\n"
		if req.Risk == "stable" {
			msg += "\n" + preRelWarn
		}
		msg += "\n" + moreInfoHint
		return msg
	}

	// there are matching arch+risk releases => give hints and instructions
	// about these other tracks
	if hits := matches["architecture:risk"]; len(hits) != 0 {
		// TRANSLATORS: %q is for the snap name, %s is the full requested channel
		msg := fmt.Sprintf(i18n.G("snap %q is not available on %s but is available to install on the following tracks:\n"), snapName, req.Full())
		msg += installTable(snapName, action, hits, true)
		msg += "\n\n" + trackWarn
		msg += "\n" + moreInfoHint
		return msg
	}

	// generic error
	// TRANSLATORS: %q is for the snap name, %s is the full requested channel
	msg := fmt.Sprintf(i18n.G("snap %q is not available on %s but other tracks exist.\n"), snapName, req.Full())
	msg += "\n\n" + trackWarn
	msg += "\n" + moreInfoHint
	return msg
}

func installTable(snapName, action string, avail []*snap.Channel, full bool) string {
	b := &bytes.Buffer{}
	w := tabwriter.NewWriter(b, len("candidate")+2, 1, 2, ' ', 0)
	first := true
	for _, a := range avail {
		if first {
			first = false
		} else {
			fmt.Fprint(w, "\n")
		}
		var ch string
		if full {
			ch = a.Full()
		} else {
			ch = a.String()
		}
		chOption := channelOption(a)
		fmt.Fprintf(w, "%s\tsnap %s %s %s", ch, action, chOption, snapName)
	}
	w.Flush()
	tbl := b.String()
	// indent to drive fill/ToText to keep the tabulations intact
	lines := strings.SplitAfter(tbl, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "")
}

func channelOption(c *snap.Channel) string {
	if c.Branch == "" {
		if c.Track == "" {
			return fmt.Sprintf("--%s", c.Risk)
		}
		if c.Risk == "stable" {
			return fmt.Sprintf("--channel=%s", c.Track)
		}
	}
	return fmt.Sprintf("--channel=%s", c)
}

func archsForChannels(cs []*snap.Channel) []string {
	archs := []string{}
	for _, c := range cs {
		if !strutil.ListContains(archs, c.Architecture) {
			archs = append(archs, c.Architecture)
		}
	}
	return archs
}

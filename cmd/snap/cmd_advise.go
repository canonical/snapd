// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

type cmdAdviseSnap struct {
	Positionals struct {
		CommandOrPkg string
	} `positional-args:"true"`

	Format string `long:"format" default:"pretty" choice:"pretty" choice:"json"`
	// Command makes advise try to find snaps that provide this command
	Command bool `long:"command"`

	// FromApt tells advise that it got started from an apt hook
	// and needs to communicate over a socket
	FromApt bool `long:"from-apt"`
}

var shortAdviseSnapHelp = i18n.G("Advise on available snaps")
var longAdviseSnapHelp = i18n.G(`
The advise-snap command searches for and suggests the installation of snaps.

If --command is given, it suggests snaps that provide the given command.
Otherwise it suggests snaps with the given name.
`)

func init() {
	cmd := addCommand("advise-snap", shortAdviseSnapHelp, longAdviseSnapHelp, func() flags.Commander {
		return &cmdAdviseSnap{}
	}, map[string]string{
		"command":  i18n.G("Advise on snaps that provide the given command"),
		"from-apt": i18n.G("Advise will talk to apt via an apt hook"),
		"format":   i18n.G("Use the given output format"),
	}, []argDesc{
		{name: "<command or pkg>"},
	})
	cmd.hidden = true
}

func outputAdviseExactText(command string, result []advisor.Command) error {
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, i18n.G("Command %q not found, but can be installed with:\n"), command)
	fmt.Fprintf(Stdout, "\n")
	for _, snap := range result {
		fmt.Fprintf(Stdout, "sudo snap install %s\n", snap.Snap)
	}
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, "See 'snap info <snap name>' for additional versions.\n")
	fmt.Fprintf(Stdout, "\n")
	return nil
}

func outputAdviseMisspellText(command string, result []advisor.Command) error {
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, i18n.G("Command %q not found, did you mean:\n"), command)
	fmt.Fprintf(Stdout, "\n")
	for _, snap := range result {
		fmt.Fprintf(Stdout, " command %q from snap %q\n", snap.Command, snap.Snap)
	}
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, "See 'snap info <snap name>' for additional versions.\n")
	fmt.Fprintf(Stdout, "\n")
	return nil
}

func outputAdviseJSON(command string, results []advisor.Command) error {
	enc := json.NewEncoder(Stdout)
	enc.Encode(results)
	return nil
}

type jsonRPC struct {
	JsonRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Command         string   `json:"command"`
		SearchTerms     []string `json:"search-terms"`
		UnknownPackages []string `json:"unknown-packages"`
	}
}

// readRpc reads a apt json rpc protocol 0.1 message as described in
// https://salsa.debian.org/apt-team/apt/blob/master/doc/json-hooks-protocol.md#wire-protocol
func readRpc(r *bufio.Reader) (*jsonRPC, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("cannot read json-rpc: %v", err)
	}
	if osutil.GetenvBool("SNAP_APT_HOOK_DEBUG") {
		fmt.Fprintf(os.Stderr, "%s\n", line)
	}

	var rpc jsonRPC
	if err := json.Unmarshal(line, &rpc); err != nil {
		return nil, err
	}
	// empty \n
	emptyNL, _, err := r.ReadLine()
	if err != nil {
		return nil, err
	}
	if string(emptyNL) != "" {
		return nil, fmt.Errorf("unexpected line: %q (empty)", emptyNL)
	}

	return &rpc, nil
}

func adviseViaAptHook() error {
	sockFd := os.Getenv("APT_HOOK_SOCKET")
	if sockFd == "" {
		return fmt.Errorf("cannot find APT_HOOK_SOCKET env")
	}
	fd, err := strconv.Atoi(sockFd)
	if err != nil {
		return fmt.Errorf("expected APT_HOOK_SOCKET to be a decimal integer, found %q", sockFd)
	}

	f := os.NewFile(uintptr(fd), "apt-hook-socket")
	if f == nil {
		return fmt.Errorf("cannot open file descriptor %v", fd)
	}
	defer f.Close()

	conn, err := net.FileConn(f)
	if err != nil {
		return fmt.Errorf("cannot connect to %v: %v", fd, err)
	}
	defer conn.Close()

	r := bufio.NewReader(conn)

	// handshake
	rpc, err := readRpc(r)
	if err != nil {
		return err
	}
	if rpc.Method != "org.debian.apt.hooks.hello" {
		return fmt.Errorf("expected 'hello' method, got: %v", rpc.Method)
	}
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":0,"result":{"version":"0.1"}}` + "\n\n")); err != nil {
		return err
	}

	// payload
	rpc, err = readRpc(r)
	if err != nil {
		return err
	}
	if rpc.Method == "org.debian.apt.hooks.install.fail" {
		for _, pkgName := range rpc.Params.UnknownPackages {
			match, err := advisor.FindPackage(pkgName)
			if err == nil && match != nil {
				fmt.Fprintf(Stdout, "\n")
				fmt.Fprintf(Stdout, i18n.G("No apt package %q, but there is a snap with that name.\n"), pkgName)
				fmt.Fprintf(Stdout, i18n.G("Try \"snap install %s\"\n"), pkgName)
				fmt.Fprintf(Stdout, "\n")
			}
		}

	}
	if rpc.Method == "org.debian.apt.hooks.search.post" {
		// FIXME: do a snap search here
		// FIXME2: figure out why apt does not tell us the search results
	}

	// bye
	rpc, err = readRpc(r)
	if err != nil {
		return err
	}
	if rpc.Method != "org.debian.apt.hooks.bye" {
		return fmt.Errorf("expected 'bye' method, got: %v", rpc.Method)
	}

	return nil
}

func (x *cmdAdviseSnap) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.FromApt {
		return adviseViaAptHook()
	}

	if len(x.Positionals.CommandOrPkg) == 0 {
		return fmt.Errorf("the required argument `<command or pkg>` was not provided")
	}

	if x.Command {
		return adviseCommand(x.Positionals.CommandOrPkg, x.Format)
	}

	return advisePkg(x.Positionals.CommandOrPkg)
}

func advisePkg(pkgName string) error {
	match, err := advisor.FindPackage(pkgName)
	if err != nil {
		return fmt.Errorf("advise for pkgname failed: %s", err)
	}
	if match != nil {
		fmt.Fprintf(Stdout, i18n.G("Packages matching %q:\n"), pkgName)
		fmt.Fprintf(Stdout, " * %s - %s\n", match.Snap, match.Summary)
		fmt.Fprintf(Stdout, i18n.G("Try: snap install <selected snap>\n"))
	}

	// FIXME: find mispells

	return nil
}

func adviseCommand(cmd string, format string) error {
	// find exact matches
	matches, err := advisor.FindCommand(cmd)
	if err != nil {
		return fmt.Errorf("advise for command failed: %s", err)
	}
	if len(matches) > 0 {
		switch format {
		case "json":
			return outputAdviseJSON(cmd, matches)
		case "pretty":
			return outputAdviseExactText(cmd, matches)
		default:
			return fmt.Errorf("unsupported format %q", format)
		}
	}

	// find misspellings
	matches, err = advisor.FindMisspelledCommand(cmd)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		switch format {
		case "json":
			return outputAdviseJSON(cmd, matches)
		case "pretty":
			return outputAdviseMisspellText(cmd, matches)
		default:
			return fmt.Errorf("unsupported format %q", format)
		}
	}

	return fmt.Errorf("%s: command not found", cmd)
}

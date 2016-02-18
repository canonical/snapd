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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"
	//	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
)

var shortConfigHelp = i18n.G("Configure an active snap")
var longConfigHelp = i18n.G(`
The config command configures an active snap.

If you invoke config providing only the snap name, a full configuration dump of
the snap will be output. You may then edit this output and provide it to the
--file option to modify the configuration.

If the snap follows the recommended approach for configuration, using a YAML
document that is a mapping which keys are strings, you can specify a
hierarchical period-separated path (rooted just under the package in the yaml
document) to obtain just that vallue. For example, if

.    snap config hello-world.canonical

were to output

.    config:
.      hello-world:
.        foo:
.          bar: hello
.          baz: world

then

.    snap config hello-world.canonical foo.bar

would output just the word "hello".

Similarly,

.    snap config hello-world.canonical foo.bar=bye

would change just that part of the configuration.

Note that at the moment you can't combine both approaches, nor can you provide
more than one configuration snippet at a time.
`)

type cmdConfig struct {
	File       flags.Filename `long:"file" description:"a file from which to read the configuration; use '-' to read from standard input."`
	Positional struct {
		Snap    string   `positional-arg-name:"<snap>" description:"the full name of the package, that must be active"`
		Snippet []string `positional-arg-name:"<snippet>" required:"0" description:"a configuration snippet"`
	} `positional-args:"yes" required:"yes"`
}

func (cmdConfig) Usage() string {
	return "[--file=FILE]"
}

func init() {
	addCommand("config", shortConfigHelp, longConfigHelp, func() interface{} { return &cmdConfig{} })
}

var (
	errOnlyOneSnippet    = errors.New(i18n.G("we only support one snippet (for now)"))
	errNoSnippetWithFile = errors.New(i18n.G("we don't support specifying a snippet and a file (for now)"))
)

func (x *cmdConfig) Execute([]string) error {
	cli := Client()

	switch len(x.Positional.Snippet) {
	case 0:
		var f io.Reader
		if x.File == "-" {
			f = Stdin
		} else if x.File != "" {
			var err error
			f, err = os.Open(string(x.File))
			if err != nil {
				return err
			}
		}

		r, err := cli.ConfigFromReader(x.Positional.Snap, f)
		if err != nil {
			return err
		}

		_, err = io.Copy(Stdout, r)
		return err

	case 1:
		if x.File != "" {
			// TODO: support specifying a non-setting snippet
			// together with --file, meaning "set the whole thing,
			// and tell me the value of x" (this is currently
			// unsupported all up and down the stack).
			return errNoSnippetWithFile
		}

		value := ""
		path := x.Positional.Snippet[0]
		idx := strings.IndexByte(path, '=')
		if idx > -1 {
			value = path[idx+1:]
			path = path[:idx]
		}

		out, err := cli.ConfigFromSnippet(x.Positional.Snap, path, value)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stdout, out)

	default:
		// TODO: think (and implement) the semantics of n > 1 snippets.
		return errOnlyOneSnippet
	}

	return nil
}

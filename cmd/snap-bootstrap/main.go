// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/logger"
)

var (
	shortHelp = "Bootstrap a Ubuntu Core system"
	longHelp  = `
snap-bootstrap is a tool to bootstrap Ubuntu Core from ephemeral systems
such as initramfs.
`

	opts            struct{}
	commandBuilders []func(*flags.Parser)
)

func main() {
	mylog.Check(run(os.Args[1:]))
}

func run(args []string) error {
	if os.Getuid() != 0 {
		return fmt.Errorf("please run as root")
	}
	logger.BootSetup()
	return parseArgs(args)
}

func parseArgs(args []string) error {
	p := parser()

	_ := mylog.Check2(p.ParseArgs(args))

	return err
}

func parser() *flags.Parser {
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	p.ShortDescription = shortHelp
	p.LongDescription = longHelp
	for _, builder := range commandBuilders {
		builder(p)
	}
	return p
}

func addCommandBuilder(builder func(*flags.Parser)) {
	commandBuilders = append(commandBuilders, builder)
}

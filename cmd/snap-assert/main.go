// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"io/ioutil"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/tool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "snap-assert: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	var opts struct {
		Positional struct {
			AssertionType string `positional-arg-name:"<assert-type>" required:"yes" description:"type of the assertion to sign (mandatory)"`
			Statement     string `positional-arg-name:"<statement>" description:"input file with the statement to sign as YAML or JSON (optional, left out or - means use stdin)"`
		} `positional-args:"yes"`

		Format string `long:"format" default:"yaml" description:"the format of the input statement (json|yaml)"`

		AuthorityID string `long:"authority-id" description:"identifier of the signer (otherwise taken from the account-key or the statement)"`
		KeyID       string `long:"key-id" description:"long key id of the GnuPG key to use (otherwise taken from account-key)"`
		AccountKey  string `long:"account-key" description:"file with the account-key assertion of the key to use"`

		Revision int `long:"revision" description:"revision to set for the assertion (starts and defaults to 0)"`

		GPGHomedir string `long:"gpg-homedir" description:"alternative GPG homedir, otherwise the default ~/.gnupg is used"`
	}

	parser := flags.NewParser(&opts, flags.HelpFlag)

	_, err := parser.Parse()
	if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
		parser.WriteHelp(os.Stdout)
		return nil
	} else if err != nil {
		return err
	}

	var mediaType string
	switch opts.Format {
	case "yaml":
		mediaType = tool.YAMLInput
	case "json":
		mediaType = tool.JSONInput
	default:
		return fmt.Errorf("input format can only be yaml or json")
	}

	var accountKey []byte
	if opts.AccountKey != "" {
		var err error
		accountKey, err = ioutil.ReadFile(opts.AccountKey)
		if err != nil {
			return fmt.Errorf("cannot read account-key: %v", err)
		}
	}

	statement, err := readStatement(opts.Positional.Statement)
	if err != nil {
		return fmt.Errorf("cannot read statement: %v", err)
	}

	keypairMgr := asserts.NewGPGKeypairManager(opts.GPGHomedir)

	signReq := tool.SignRequest{
		AccountKey:         accountKey,
		KeyID:              strings.ToLower(opts.KeyID),
		AuthorityID:        opts.AuthorityID,
		AssertionType:      opts.Positional.AssertionType,
		StatementMediaType: mediaType,
		Statement:          statement,
		Revision:           opts.Revision,
	}

	encodedAssert, err := tool.Sign(&signReq, keypairMgr)
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(encodedAssert)
	if err != nil {
		return err
	}
	return nil
}

func readStatement(statementFile string) ([]byte, error) {
	if statementFile == "" || statementFile == "-" {
		return ioutil.ReadAll(os.Stdin)
	} else {
		return ioutil.ReadFile(statementFile)
	}
}

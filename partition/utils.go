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
package partition

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Run the commandline specified by the args array chrooted to the given dir
var runInChroot = func(chrootDir string, args ...string) (err error) {
	fullArgs := []string{"/usr/sbin/chroot", chrootDir}
	fullArgs = append(fullArgs, args...)

	return runCommand(fullArgs...)
}

// FIXME: would it make sense to differenciate between launch errors and
//        exit code? (i.e. something like (returnCode, error) ?)
func runCommandImpl(args ...string) (err error) {
	if len(args) == 0 {
		return errors.New("no command specified")
	}

	// FIXME: use logger
	/*
		if debug == true {

			log.debug('running: {}'.format(args))
		}
	*/

	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		cmdline := strings.Join(args, " ")
		return fmt.Errorf("Failed to run command '%s': %s (%s)",
			cmdline, out, err)
	}
	return nil
}

// Run the command specified by args
// This is a var instead of a function to making mocking in the tests easier
var runCommand = runCommandImpl

// Run command specified by args and return the output
func runCommandWithStdoutImpl(args ...string) (output string, err error) {
	if len(args) == 0 {
		return "", errors.New("no command specified")
	}

	bytes, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "", err
	}

	return string(bytes), err
}

// This is a var instead of a function to making mocking in the tests easier
var runCommandWithStdout = runCommandWithStdoutImpl

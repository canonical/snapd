package partition

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Return nil if given path exists.
func fileExists(path string) (err error) {
	_, err = os.Stat(path)

	return err
}

// Return true if the given path exists and is a directory
func isDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}

// Run the commandline specified by the args array chrooted to the
// new root filesystem.
//
// Errors are fatal.
func (p *Partition) runInChroot(args []string) (err error) {
	var fullArgs []string

	fullArgs = append(fullArgs, "/usr/sbin/chroot")
	fullArgs = append(fullArgs, p.MountTarget)

	fullArgs = append(fullArgs, args...)

	return runCommand(fullArgs)
}

// Run the command specified by args
var runCommand = func(args []string) (err error) {
	if len(args) == 0 {
		return errors.New("ERROR: no command specified")
	}

	// FIXME: use logger
	/*
		if debug == true {

			log.debug('running: {}'.format(args))
		}
	*/

	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		cmdline := strings.Join(args, " ")
		return errors.New(fmt.Sprintf("Failed to run command '%s': %s (%s)",
			cmdline,
			out,
			err))
	}
	return nil
}

// Run command specified by args and return array of output lines.
func getCommandStdout(args []string) (output []string, err error) {

	// FIXME: use logger
	/*
		if debug == true {

			log.debug('running: {}'.format(args))
		}
	*/

	bytes, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return output, err
	}

	output = strings.Split(string(bytes), "\n")

	return output, err
}

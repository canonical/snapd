package partition

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Return true if given path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return (err == nil)
}

// Return true if the given path exists and is a directory
func isDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}

// Run the command specified by args
// FIXME: would it make sense to differenciate between launch errors and
//        exit code? (i.e. something like (returnCode, error) ?)
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
// FIXME: would it make sense to make this a vararg (args...) ?
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

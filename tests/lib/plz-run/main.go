// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Zygmunt Krynicki

// Package main implements the plz-run command line utility. The program uses
// D-Bus APIs of systemd, running as the init system, in order to create a
// process elsewhere in the process hierarchy and not as a child process of the
// plz-run program. This allows plz-run to, given proper permissions are
// arranged, to run an unconfined program from a confined context (e.g. under
// restrictive apparmor profile, with an attached seccomp BPF program, with a
// set of eBPF programs attached to the cgroup hierarchy.
//
// The newly started process has connected standard input, output and error
// streams from the streams used to invoke plz-run. The exit code of the remote
// process is relayed.
//
// Supported features:
//
// - running any program with any arguments without shell expansion
// - injecting additional environment variables with the -E switch.
// - running as the given user and group with the -u and -g switches.
//
// Missing features:
//
// - Running under user slice as a user service.
// - Running as a scope.
// - Interacting with systemd --user.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
)

type EnvList []string

func (e *EnvList) String() string {
	return fmt.Sprintf("%v", *e)
}

func (e *EnvList) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("environment variables must have a key=value format, not %q", value)
	}
	*e = append(*e, value)
	return nil
}

func plz(ctx context.Context, args []string) error {
	// Constants related to systemd D-Bus interfaces.
	// Sadly most cannot be strongly typed with go-dbus, as the API relies on untyped strings.
	const (
		dbusPropsIface                                      = "org.freedesktop.DBus.Properties"
		dbusPropsPropertiesChangedMember                    = "PropertiesChanged"
		dbusPropsPropertiesChangedSignal                    = dbusPropsIface + "." + dbusPropsPropertiesChangedMember
		fdoSystemd1BusName                                  = "org.freedesktop.systemd1"
		fdoSystemd1ObjectPath               dbus.ObjectPath = "/org/freedesktop/systemd1"
		fdoSystemd1ManagerIface                             = fdoSystemd1BusName + ".Manager"
		fdoSystemd1ServiceIface                             = fdoSystemd1BusName + ".Service"
		fdoSystemd1StartTransientUnitMethod                 = fdoSystemd1ManagerIface + ".StartTransientUnit"
		fdoSystemd1ResetFailedUnitMethod                    = fdoSystemd1ManagerIface + ".ResetFailedUnit"
		fdoSystemd1ManagerJobRemovedMember                  = "JobRemoved"
		fdoSystemd1ManagerJobRemovedSignal                  = fdoSystemd1ManagerIface + "." + fdoSystemd1ManagerJobRemovedMember
	)

	// Parse arguments.
	fl := flag.NewFlagSet("plz-run", flag.ContinueOnError)
	var (
		user, group string
		env         EnvList
		pamName     string
		workingDir  string
		sameDir     bool
	)
	fl.StringVar(&user, "u", "", "Ask systemd to use given User=")
	fl.StringVar(&group, "g", "", "Ask systemd to use given Group=")
	fl.Var(&env, "E", "Ask systemd use the given Environment= (can be used multiple times)")
	fl.StringVar(&pamName, "pam", "", "Ask systemd to use given name as PAMName=")
	fl.StringVar(&workingDir, "C", "", "Ask systemd to use the given WorkingDirectory=")
	fl.BoolVar(&sameDir, "same-dir", false, "Same as -C=$CURDIR")
	fl.Usage = func() {
		fmt.Fprintf(fl.Output(), "Usage: %s [OPTIONS] PROG [ARGS]\n", fl.Name())
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return err
	}
	if sameDir && workingDir != "" {
		return errors.New("cannot use both -same-dir and -C")
	}

	if fl.NArg() == 0 {
		fl.Usage()
		return flag.ErrHelp
	}

	// Preserve the current working directory if requested.
	if sameDir {
		if d, err := os.Getwd(); err != nil {
			return err
		} else {
			workingDir = d
		}
	}

	// Find the program the user wants to run.
	progPath := fl.Arg(0)
	if !filepath.IsAbs(progPath) {
		var err error
		progPath, err = exec.LookPath(progPath)
		if err != nil {
			return err
		}
	}
	progArgs := fl.Args()

	// Pick a random number as our unique element of the service we're about to start.
	cookie := rand.Int()

	// Connect to the D-Bus system bus.
	// Use a sequential signal handler so that we always see the signals in the order they are delivered.
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx), dbus.WithSignalHandler(dbus.NewSequentialSignalHandler()))
	if err != nil {
		return err
	}
	defer conn.Close()

	// Ask DBus broker to relay the JobRemoved signal as sent by systemd.
	matchJobRemovedExpr := []dbus.MatchOption{
		dbus.WithMatchSender(fdoSystemd1BusName),                 // match the bus name of systemd,
		dbus.WithMatchObjectPath(fdoSystemd1ObjectPath),          // match the sender at the object path of systemd.
		dbus.WithMatchInterface(fdoSystemd1ManagerIface),         // match the Manager interface name.
		dbus.WithMatchMember(fdoSystemd1ManagerJobRemovedMember), // match the JobRemoved interface member.
	}
	conn.AddMatchSignalContext(ctx, matchJobRemovedExpr...)
	defer conn.RemoveMatchSignalContext(ctx, matchJobRemovedExpr...)

	// Ask DBus broker to relay the PropertiesChanged signal as sent by systemd's job.
	matchPropsChangedExpr := []dbus.MatchOption{
		dbus.WithMatchSender(fdoSystemd1BusName), // match the bus name of systemd,
		dbus.WithMatchObjectPath(dbus.ObjectPath(fmt.Sprintf("%s/unit/plz_2drun_2d%d_2eservice", fdoSystemd1ObjectPath, cookie))),
		dbus.WithMatchInterface(dbusPropsIface),                // match the Properties interface name.
		dbus.WithMatchMember(dbusPropsPropertiesChangedMember), // match the PropertiesChanged interface member.
		dbus.WithMatchArg(0, fdoSystemd1ServiceIface),          // match only messages whose first body item, the interface name, is that of .Service.
	}
	conn.AddMatchSignalContext(ctx, matchPropsChangedExpr...)
	defer conn.RemoveMatchSignalContext(ctx, matchPropsChangedExpr...)

	// Arrange go-dbus to deliver signals to the given channel.
	sigCh := make(chan *dbus.Signal)
	conn.Signal(sigCh) // sigCh is closed when conn is closed.
	defer conn.RemoveSignal(sigCh)

	// Start the transient unit that corresponds to our workload and get the resulting object path.
	flags := dbus.Flags(0)
	name := fmt.Sprintf("plz-run-%d.service", cookie)
	mode := "fail"

	type Prop struct {
		Name  string
		Value dbus.Variant
	}
	props := []Prop{
		{Name: "Description", Value: dbus.MakeVariant("potato")},
		{Name: "Type", Value: dbus.MakeVariant("oneshot")},
		// We cannot rely on CollectMode as it is not available on old systemd.
		// {Name: "CollectMode", Value: dbus.MakeVariant("inactive-or-failed")},
		{Name: "StandardInputFileDescriptor", Value: dbus.MakeVariant(dbus.UnixFD(os.Stdin.Fd()))},
		{Name: "StandardOutputFileDescriptor", Value: dbus.MakeVariant(dbus.UnixFD(os.Stdout.Fd()))},
		{Name: "StandardErrorFileDescriptor", Value: dbus.MakeVariant(dbus.UnixFD(os.Stderr.Fd()))},
		{
			Name: "ExecStart", Value: dbus.MakeVariant([]struct {
				Path          string
				Args          []string
				IgnoreFailure bool
			}{{Path: progPath, Args: progArgs}}),
		},
	}
	if user != "" {
		props = append(props, Prop{Name: "User", Value: dbus.MakeVariant(user)})
	}
	if group != "" {
		props = append(props, Prop{Name: "Group", Value: dbus.MakeVariant(group)})
	}
	if len(env) > 0 {
		props = append(props, Prop{Name: "Environment", Value: dbus.MakeVariant(env)})
	}
	if pamName != "" {
		props = append(props, Prop{Name: "PAMName", Value: dbus.MakeVariant(pamName)})
	}
	if workingDir != "" {
		props = append(props, Prop{Name: "WorkingDirectory", Value: dbus.MakeVariant(workingDir)})
	}
	// The slice of auxiliary units is required by the API but unused.
	var aux []struct {
		Name       string
		Properties []Prop
	}
	var ourJobPath dbus.ObjectPath
	obj := conn.Object(fdoSystemd1BusName, fdoSystemd1ObjectPath)
	if err := obj.CallWithContext(ctx, fdoSystemd1StartTransientUnitMethod, flags, name, mode, props, aux).Store(&ourJobPath); err != nil {
		return fmt.Errorf("cannot call StartTransientUnit: %w", err)
	}

	// Iterate through the signals we've received from D-Bus.
	var (
		execMainCode   int32
		execMainStatus int32
		result         string
		jobRemoved     bool
	)

	// Wait for the job to be removed and for a result to settle.
	for result == "" || execMainCode == 0 || !jobRemoved {
		select {
		case sig := <-sigCh:
			switch sig.Name {
			case dbusPropsPropertiesChangedSignal:
				// When we receive properties changed signal, look for
				// ExecMainCode, ExecMainStatus and Result properties of the
				// .Service interface in order to store the exit code.
				var (
					propsIface       string
					propsChanged     map[string]dbus.Variant
					propsInvalidated []string
				)
				if err := dbus.Store(sig.Body, &propsIface, &propsChanged, &propsInvalidated); err != nil {
					return err
				}

				// Certain properties are interesting to us.
				type nameStorage struct {
					iface     string
					name      string
					storage   any
					storeZero func()
				}
				var (
					interestingProps []nameStorage = []nameStorage{
						{fdoSystemd1ServiceIface, "ExecMainCode", &execMainCode, func() { execMainCode = 0 }},
						{fdoSystemd1ServiceIface, "ExecMainStatus", &execMainStatus, func() { execMainStatus = 0 }},
						{fdoSystemd1ServiceIface, "Result", &result, func() { result = "" }},
					}
				)

				// Store the subset of properties we are interested in.
				for _, prop := range interestingProps {
					if prop.iface != propsIface {
						continue
					}
					if val, ok := propsChanged[prop.name]; ok {
						if err := val.Store(prop.storage); err != nil {
							return fmt.Errorf("cannot store %s: %w", prop.name, err)
						}
					}
					for _, p := range propsInvalidated {
						if prop.name == p {
							prop.storeZero()
						}
					}
				}
			case fdoSystemd1ManagerJobRemovedSignal:
				// When we receive the JobRemoved signal corresponding to our
				// job we may not yet be ready to return, as the property
				// change with the exit code of the main process of the service
				// arrives separately and after.
				var (
					jobId     uint32
					jobPath   dbus.ObjectPath
					jobUnit   string
					jobResult string
				)
				if err := dbus.Store(sig.Body, &jobId, &jobPath, &jobUnit, &jobResult); err != nil {
					return err
				}
				if jobPath == ourJobPath {
					jobRemoved = true
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Relay ExecMainStatus exit code back to the caller.
	switch result {
	case "exit-code":
		if execMainStatus > 0 {
			if err := obj.CallWithContext(ctx, fdoSystemd1ResetFailedUnitMethod, flags, name).Store(); err != nil {
				return fmt.Errorf("cannot call ResetFailedUnit: %w", err)
			}

			return SilentError(uint8(execMainStatus))
		}
		return nil
	case "success":
		// This includes services that returned non-zero but expected exit code.
		return nil
	case "failure":
		return SilentError(1)
	case "signal":
		return fmt.Errorf("killed by signal %d", execMainStatus)
	case "core-dump":
		return errors.New("program dumped core")
	default:
		return fmt.Errorf("unhandled systemd job result value: %v", result)
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := plz(ctx, os.Args[1:])
	if err != nil {
		if err, ok := err.(SilentError); ok {
			os.Exit(int(err))
		}

		fmt.Fprintf(os.Stderr, "%s error: %s\n", filepath.Base(os.Args[0]), err)
		os.Exit(1)
	}
}

// SilentError is an error type that produces a given error code but no error message.
type SilentError uint8

// Error returns the empty string.
func (e SilentError) Error() string {
	return ""
}

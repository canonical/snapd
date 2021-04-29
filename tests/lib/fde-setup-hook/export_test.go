package main

import (
	"io"
)

var (
	RunFdeSetup     = runFdeSetup
	RunFdeRevealKey = runFdeRevealKey

	TestKeyHandle = testKeyHandle
	Xor13         = xor13
)

func MockStdinStdout(mockedStdin io.Reader, mockedStdout io.Writer) (restore func()) {
	oldOsStdin := osStdin
	oldOsStdout := osStdout
	osStdin = mockedStdin
	osStdout = mockedStdout
	return func() {
		osStdin = oldOsStdin
		osStdout = oldOsStdout
	}
}

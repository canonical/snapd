# Bonjour Conformance Test

The goal is to make `dnssd` fully compliant with [Bonjour](https://developer.apple.com/bonjour/) from Apple. We are using *Bonjour Conformance Test* (v1.5.0) to test our mDNS responder implementation.

I'm using a MacBook Pro running macOSS 10.12 (or higher) as a test machine, which is connected to a router. 
The tested device is a Raspberry Pi 3 Model B also connected to the router.

There is a test implementation of a mDNS responder in `_cmd/bct/main.go` which is compiled for the RPi with `GOOS=linux GOARCH=arm GOARM=7 go build -o bct main.go`.
Run the executable `bct` on the RPi while running multicast DNS tests (withou hot plugging – see #9) on the test machine with `sudo ./BonjourConformanceTest -S -M h -DD -E <router-ip>`.

The latest test results can be found in `ConformanceTestResults`.
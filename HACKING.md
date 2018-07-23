# Hacking on snapd

Hacking on snapd is fun and straightforward. The code is extensively
unit tested and we use the [spread](https://github.com/snapcore/spread)
integration test framework for the integration/system level tests.

## Development

### Supported Go versions

snapd is supported on Go 1.6 onwards.

### Setting up a GOPATH

When working with the source of Go programs, you should define a path within
your home directory (or other workspace) which will be your `GOPATH`. `GOPATH`
is similar to Java's `CLASSPATH` or Python's `~/.local`. `GOPATH` is documented
[online](http://golang.org/pkg/go/build/) and inside the go tool itself

    go help gopath

Various conventions exist for naming the location of your `GOPATH`, but it
should exist, and be writable by you. For example

    export GOPATH=${HOME}/work
    mkdir $GOPATH

will define and create `$HOME/work` as your local `GOPATH`. The `go` tool
itself will create three subdirectories inside your `GOPATH` when required;
`src`, `pkg` and `bin`, which hold the source of Go programs, compiled packages
and compiled binaries, respectively.

Setting `GOPATH` correctly is critical when developing Go programs. Set and
export it as part of your login script.

Add `$GOPATH/bin` to your `PATH`, so you can run the go programs you install:

    PATH="$PATH:$GOPATH/bin"

(note `$GOPATH` can actually point to multiple locations, like `$PATH`, so if
your `$GOPATH` is more complex than a single entry you'll need to adjust the
above).

### Getting the snapd sources

The easiest way to get the source for `snapd` is to use the `go get` command.

    go get -d -v github.com/snapcore/snapd/...

This command will checkout the source of `snapd` and inspect it for any unmet
Go package dependencies, downloading those as well. `go get` will also build
and install `snapd` and its dependencies. To also build and install `snapd`
itself into `$GOPATH/bin`, omit the `-d` flag. More details on the `go get`
flags are available using

    go help get

At this point you will have the git local repository of the `snapd` source at
`$GOPATH/src/github.com/snapcore/snapd`. The source for any
dependent packages will also be available inside `$GOPATH`.

### Dependencies handling

Go dependencies are handled via `govendor`. Get it via:

    go get -u github.com/kardianos/govendor

After a fresh checkout, move to the snapd source directory:

    cd $GOPATH/src/github.com/snapcore/snapd

And then, run:

    govendor sync

You can use the script `get-deps.sh` to run the two previous steps.

If a dependency need updating

    govendor fetch github.com/path/of/dependency

Other dependencies are handled via distribution packages and you should ensure
that dependencies for your distribution are installed. For example, on Ubuntu,
run:

    sudo apt-get build-dep ./

### Building

To build, once the sources are available and `GOPATH` is set, you can just run

    go build -o /tmp/snap github.com/snapcore/snapd/cmd/snap

to get the `snap` binary in /tmp (or without -o to get it in the current
working directory). Alternatively:

    go install github.com/snapcore/snapd/cmd/snap/...

to have it available in `$GOPATH/bin`

Similarly, to build the `snapd` REST API daemon, you can run

    go build -o /tmp/snapd github.com/snapcore/snapd/cmd/snapd

### Contributing

Contributions are always welcome! Please make sure that you sign the
Canonical contributor license agreement at
http://www.ubuntu.com/legal/contributors

Snapd can be found on Github, so in order to fork the source and contribute,
go to https://github.com/snapcore/snapd. Check out [Github's help
pages](https://help.github.com/) to find out how to set up your local branch,
commit changes and create pull requests.

We value good tests, so when you fix a bug or add a new feature we highly
encourage you to create a test in `$source_test.go`. See also the section
about Testing.

### Testing

To run the various tests that we have to ensure a high quality source just run:

    ./run-checks

This will check if the source format is consistent, that it builds, all tests
work as expected and that "go vet" has nothing to complain.

The source format follows the `gofmt -s` formating. Please run this on your sources files if `run-checks` complains about the format.

You can run individual test for a sub-package by changing into that directory and:

    go test -check.f $testname

If a test hangs, you can enable verbose mode:

    go test -v -check.vv

(or -check.v for less verbose output).

There is more to read about the testing framework on the [website](https://labix.org/gocheck)

### Running the spread tests

To run the spread tests locally you need the latest version of spread
from https://github.com/snapcore/spread. It can be installed via:

    $ sudo apt install qemu-kvm autopkgtest
    $ sudo snap install --devmode spread

Then setup the environment via:

    $ mkdir -p .spread/qemu
    $ cd .spread/qemu
    # For xenial (same works for yakkety/zesty)
    $ adt-buildvm-ubuntu-cloud -r xenial
    $ mv adt-xenial-amd64-cloud.img ubuntu-16.04.img
    # For trusty
    $ adt-buildvm-ubuntu-cloud -r trusty --post-command='sudo apt-get install -y --install-recommends linux-generic-lts-xenial && update-grub'
    $ mv adt-trusty-amd64-cloud.img ubuntu-14.04-64.img


And you can run the tests via:

    $ spread -v qemu:

For quick reuse you can use:

    $ spread -reuse qemu:

It will print how to reuse the systems. Make sure to use
`export REUSE_PROJECT=1` in your environment too.


### Testing snapd

To test the `snapd` REST API daemon on a snappy system you need to
transfer it to the snappy system and then run:

    sudo systemctl stop snapd.service snapd.socket
    sudo SNAPD_DEBUG=1 SNAPD_DEBUG_HTTP=3 ./snapd

To debug interaction with the snap store, you can set `SNAP_DEBUG_HTTP`.
It is a bitfield: dump requests: 1, dump responses: 2, dump bodies: 4.

(make hack: In case you get some security profiles errors when trying to install or refresh a snap, 
maybe you need to replace system installed snap-seccomp with the one aligned to the snapd that 
you are testing. To do this, simply backup /usr/lib/snapd/snap-seccomp and overwrite it with 
the testing one. Don't forget to rollback to the original when finish testing)

# Quick intro to hacking on snap-confine

Hey, welcome to the nice, low-level world of snap-confine

## Building the code locally

To get started from a pristine tree you want to do this:

```
./mkversion.sh
cd cmd/
autoreconf -i -f
./configure --prefix=/usr --libexecdir=/usr/lib/snapd --enable-nvidia-ubuntu
```

This will drop makefiles and let you build stuff. You may find the `make hack`
target, available in `cmd/snap-confine` handy, it installs the locally built
version on your system and reloads the apparmor profile.

## Submitting patches

Please run `make fmt` before sending your patches.

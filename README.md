# snappy

Snappy is part of Ubuntu Core and enables a fully transactional Ubuntu sytem.

## Development

### Setting up a GOPATH

When working with the source of Go programs, you should define a path within
your home directory (or other workspace) which will be your `GOPATH`. `GOPATH`
is similar to Java's `CLASSPATH` or Python's `~/.local`. `GOPATH` is documented
[http://golang.org/pkg/go/build/](online) and inside the go tool itself

    go help gopath

Various conventions exist for naming the location of your `GOPATH`, but it
should exist, and be writable by you. For example

    export GOPATH=${HOME}/work mkdir $GOPATH

will define and create `$HOME/work` as your local `GOPATH`. The `go` tool
itself will create three subdirectories inside your `GOPATH` when required;
`src`, `pkg` and `bin`, which hold the source of Go programs, compiled packages
and compiled binaries, respectively.

Setting `GOPATH` correctly is critical when developing Go programs. Set and
export it as part of your login script.

Add `$GOPATH/bin` to your `PATH`, so you can run the go programs you install:

    PATH="$PATH:$GOPATH/bin"

### Getting the snappy sources

The easiest way to get the source for `snappy` is to use the `go get` command.

    go get -d -v launchpad.net/snappy-ubuntu/snappy-golang/...

This command will checkout the source of `snappy` and inspect it for any unmet
Go package dependencies, downloading those as well. `go get` will also build
and install `snappy` and its dependencies. To checkout without installing, use
the `-d` flag. More details on the `go get` flags are available using

    go help get

At this point you will have the git local repository of the `snappy` source at
`$GOPATH/launchpad.net/snappy-ubuntu/snappy-golang`. The source for any
dependent packages will also be available inside `$GOPATH`.

### Building

To build, once the sources are available and `GOPATH` is set, you can just run

    go build launchpad.net/snappy-ubuntu/snappy-golang/cmd/snappy

to get the `snappy` binary in your current working directory or

    go install launchpad.net/snappy-ubuntu/snappy-golang/...

to have it available in `$GOPATH/bin`

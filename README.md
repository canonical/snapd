# snappy

Snappy is part of Ubuntu Core and enables a fully transactional Ubuntu system.

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

    go get -d -v launchpad.net/snappy/...

This command will checkout the source of `snappy` and inspect it for any unmet
Go package dependencies, downloading those as well. `go get` will also build
and install `snappy` and its dependencies. To checkout without installing, use
the `-d` flag. More details on the `go get` flags are available using

    go help get

At this point you will have the git local repository of the `snappy` source at
`$GOPATH/launchpad.net/snappy/snappy-go`. The source for any
dependent packages will also be available inside `$GOPATH`.

### Building

To build, once the sources are available and `GOPATH` is set, you can just run

    go build launchpad.net/snappy/snappy-go/cmd/snappy

to get the `snappy` binary in your current working directory or

    go install launchpad.net/snappy/...

to have it available in `$GOPATH/bin`

### Contributing

Contributions are always welcome! Please make sure that you sign the
Canonical contributor licence agreement at
http://www.ubuntu.com/legal/contributors 

To get the source and propose a merge, this is what typically needs to
be done:

     bzr branch lp:snappy my-work
     cd my-work
     [hack on mywork]
     bzr lp-propose

We value good tests, so when you fix a bug or add a new feature we highly
encourage you to create a test in $source_testing.go. See also the section
about Testing.

### Testing

To run the various tests that we have to ensure a high quality source just run:

    ./run-checks

This will check if the source format is consistent, that it build, all tests
work as expected and that "go vet" and "golint" have nothing to complain.


### Dependencies handling

To generate dependencies.tsv you need `godeps`, so

    go get launchpad.net/godeps

To obtain the correct dependencies for the project, run:

    godeps -t -u dependencies.tsv

If the dependencies need updating

    godeps -t ./... > dependencies.tsv


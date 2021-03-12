# Hacking on snapd

Hacking on snapd is fun and straightforward. The code is extensively
unit tested and we use the [spread](https://github.com/snapcore/spread)
integration test framework for the integration/system level tests.

## Development

### Supported Go versions

From snapd 2.38, snapd supports Go 1.9 and onwards. For earlier snapd 
releases, snapd supports Go 1.6.

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

Snapd can be found on GitHub, so in order to fork the source and contribute,
go to https://github.com/snapcore/snapd. Check out [GitHub's help
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

The source format follows the `gofmt -s` formating. Please run this on your 
source files if `run-checks` complains about the format.

You can run an individual test for a sub-package by changing into that 
directory and:

    go test -check.f $testname

If a test hangs, you can enable verbose mode:

    go test -v -check.vv

(or -check.v for less verbose output).

There is more to read about the testing framework on the [website](https://labix.org/gocheck)

### Running spread tests

To run the spread tests locally via QEMU, you need the latest version of
[spread](https://github.com/snapcore/spread). You can get spread, QEMU, and the
build tools to build QEMU images with:

    $ sudo apt update && sudo apt install -y qemu-kvm autopkgtest
    $ curl https://niemeyer.s3.amazonaws.com/spread-amd64.tar.gz | tar -xz -C $GOPATH/bin

#### Building spread VM images

To run the spread tests via QEMU you need to create VM images in the
`~/.spread/qemu` directory:

    $ mkdir -p ~/.spread/qemu
    $ cd ~/.spread/qemu

Assuming you are building on Ubuntu 18.04 LTS (Bionic Beaver) (or a later 
development release like Ubuntu 19.04 (Disco Dingo)), run the following to 
build a 64-bit Ubuntu 16.04 LTS (Xenial Xerus) VM to run the spread tests on:

    $ autopkgtest-buildvm-ubuntu-cloud -r xenial
    $ mv autopkgtest-xenial-amd64.img ubuntu-16.04-64.img

To build an Ubuntu 14.04 (Trusty Tahr) based VM, use:

    $ autopkgtest-buildvm-ubuntu-cloud -r trusty --post-command='sudo apt-get install -y --install-recommends linux-generic-lts-xenial && update-grub'
    $ mv autopkgtest-trusty-amd64.img ubuntu-14.04-64.img

This is because we need at least 4.4+ kernel for snapd to run on Ubuntu 14.04 
LTS, which is available through the `linux-generic-lts-xenial` package.

If you are running Ubuntu 16.04 LTS, use 
`adt-buildvm-ubuntu-cloud` instead of `autopkgtest-buildvm-ubuntu-cloud` (the
latter replaced the former in 18.04):

    $ adt-buildvm-ubuntu-cloud -r xenial
    $ mv adt-xenial-amd64-cloud.img ubuntu-16.04-64.img

#### Downloading spread VM images

Alternatively, instead of building the QEMU images manually, you can download
pre-built and somewhat maintained images from 
[spread.zygoon.pl](spread.zygoon.pl). The images will need to be extracted 
with `gunzip` and placed into `~/.spread/qemu` as above.

#### Running spread with QEMU

Finally, you can run the spread tests for Ubuntu 16.04 LTS 64-bit with:

    $ spread -v qemu:ubuntu-16.04-64

To run for a different system, replace `ubuntu-16.04-64` with a different system
name.

For quick reuse you can use:

    $ spread -reuse qemu:ubuntu-16.04-64

It will print how to reuse the systems. Make sure to use
`export REUSE_PROJECT=1` in your environment too.

#### Running UC20 spread with QEMU

Ubuntu Core 20 on amd64 has a requirement to use UEFI, so there are a few 
additional steps needed to run spread with the ubuntu-core-20-64 systems locally
using QEMU. For one, upstream spread currently does not support specifying what
kind of BIOS to use with the VM, so you have to build spread from this PR:
https://github.com/snapcore/spread/pull/95, and then use the environment 
variable `SPREAD_QEMU_BIOS` to specify an UEFI BIOS to use with the VM, for 
example the one from the OVMF package. To get OVMF on Ubuntu, you can just 
install the `ovmf` package via `apt`. After installing OVMF, you can then run 
spread like so:

    $ SPREAD_QEMU_BIOS=/usr/share/OVMF/OVMF_CODE.fd spread -v qemu:ubuntu-core-20-64

This will enable testing UC20 with the spread, albeit without secure boot 
support. None of the native UC20 tests currently require secure boot however, 
all tests around secure boot are nested, see the section below about running the
nested tests.

Also, due to the in-flux state of spread support for booting UEFI VM's like 
this, you can test ubuntu-core-20-64 only by themselves and not with any other
system concurrently since the environment variable is global for all systems in
the spread run. This will be fixed in a future release of spread.

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

### Running nested tests

Nested tests are used to validate features which cannot be tested on regular tests.

The nested test suites work different from the other test suites in snapd. In this case each test runs in a new image
which is created following the rules defined for the test.

The nested tests are executed using spread tool. See the following examples using the qemu and google backends.

    . `qemu: spread qemu-nested:ubuntu-20.04-64:tests/nested/core20/tpm`
    . `google: spread google-nested:ubuntu-20.04-64:tests/nested/core20/tpm`

The nested system in all the cases is selected based on the host system. The folloing lines show the relation between host and nested systemd (same applies for classic nested tests):

    . ubuntu-16.04-64 => ubuntu-core-16-64
    . ubuntu-18.04-64 => ubuntu-core-18-64
    . ubuntu-20.04-64 => ubuntu-core-20-64

The tools used for creating and hosting the nested vms are:

    . ubuntu-image snap is used to building the images
    . QEMU is used for the virtualization (with kvm acceleration)

Nested test suite is composed by the following 4 suites:

    classic: the nested suite contains an image of a classic system downloaded from cloud-images.ubuntu.com 
    core: it tests a core nested system and the images are generated by using ubuntu-image snap
    core20: this is similar to core suite but tests on it are focused on UC20
    manual: tests on this suite create a non generic image with spedific conditions

The nested suites use some environment variables to configure the suite and the tests inside it, the most important ones are the described bellow:

    NESTED_WORK_DIR: It is path to the directory where all the nested assets and images are stored
    NESTED_TYPE: Use core for ubuntu core nested systems or classic instead.
    NESTED_CORE_CHANNEL: The images are created using ubuntu-image snap, use it to define the default branch
    NESTED_CORE_REFRESH_CHANNEL: The images can be refreshed to a specific channel, use it to specify the channel
    NESTED_USE_CLOUD_INIT: Use cloud init to make initial system configuration instead of user assertion
    NESTED_ENABLE_KVM: Enable kvm in the qemu command line
    NESTED_ENABLE_TPM: re boot in the nested vm in case it is supported (just supported on UC20)
    NESTED_ENABLE_SECURE_BOOT: Enable secure boot in the nested vm in case it is supported (just supported on UC20)
    NESTED_BUILD_SNAPD_FROM_CURRENT: Build and use either core or snapd snapd from current branch
    NESTED_CUSTOM_IMAGE_URL: Download and use an custom image from this url


# Quick intro to hacking on snap-confine

Hey, welcome to the nice, low-level world of snap-confine


## Building the code locally

To get started from a pristine tree you want to do this:

```
./mkversion.sh
cd cmd/
autoreconf -i -f
./configure --prefix=/usr --libexecdir=/usr/lib/snapd --enable-nvidia-multiarch --with-host-arch-triplet="$(dpkg-architecture -qDEB_HOST_MULTIARCH)"
```

This will drop makefiles and let you build stuff. You may find the `make hack`
target, available in `cmd/snap-confine` handy, it installs the locally built
version on your system and reloads the apparmor profile.

Note, the above configure options assume you are on Ubuntu and are generally
necessary to run/test graphical applications with your local version of
snap-confine. The `--with-host-arch-triplet` option sets your specific 
architecture and `--enable-nvidia-multiarch` allows the host's graphics drivers
and libraries to be shared with snaps. If you are on a distro other than
Ubuntu, try `--enable-nvidia-biarch` (though you'll likely need to add further
system-specific options too).

## Submitting patches

Please run `(cd cmd; make fmt)` before sending your patches for the "C" part of
the source code.

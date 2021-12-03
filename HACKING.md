# Hacking on snapd

Hacking on `snapd` is fun and straightforward. The code is extensively unit
tested and we use the [spread](https://github.com/snapcore/spread)
integration test framework for the integration/system level tests.

## Development

### Supported Ubuntu distributions

Ubuntu 18.04 LTS or later is recommended for `snapd` development.

If you want to build or test on older versions of Ubuntu, additional steps
may be required when installing dependencies.


### Supported Go version

Go 1.13 or later is required to build `snapd`.

If you need to build older versions of snapd, please have a look at the file
`debian/control` to find out what dependencies were needed at the time
(including which version of the Go compiler).

### Setting up your build environment

If your Go environment (e.g. `GOPATH`) is already configured, you should skip
this step. The Go environment setup can be added to your shell login script
(e.g. `~/.bashrc` for bash) for automatic setup (after a terminal restart).

```
export GOPATH=${HOME}/work
mkdir -p $GOPATH
export PATH="$PATH:$GOPATH/bin"
```

### Further environment variable details

When working with the source of Go programs, you should define a path within
your home directory (or other workspace) which will be your `GOPATH`. `GOPATH`
is similar to Java's `CLASSPATH` or Python's `~/.local`. `GOPATH` is
documented [online](http://golang.org/pkg/go/build/) and inside the `go` tool
itself.

    go help gopath

Various conventions exist for naming the location of your `GOPATH`, but it
should exist, and be writable by you. For example:

    export GOPATH=${HOME}/work
    mkdir $GOPATH

will define and create `$HOME/work` as your local `GOPATH`. The `go` tool
itself will create three subdirectories inside your `GOPATH` when required;
`src`, `pkg` and `bin`, which hold the source of Go programs, compiled
packages and compiled binaries, respectively.

Setting `GOPATH` correctly is critical when developing Go programs. Set and
export it as part of your login script.

Add `$GOPATH/bin` to your `PATH`, so you can run the Go programs you install:

    PATH="$PATH:$GOPATH/bin"

(note `$GOPATH` can actually point to multiple locations, like `$PATH`, so if
your `$GOPATH` is more complex than a single entry you'll need to adjust the
above).


### Getting the snapd sources

The easiest way to get the source for `snapd` is to clone the GitHub repository
in a directory where you have read-write permissions, such as your home
directory.

    cd ~/
    git clone https://github.com/snapcore/snapd.git
    cd snapd

This will allow you to build and test `snapd`. If you wish to contribute to
the `snapd` project, please see the Contributing section.

### Installing build dependencies

Build dependencies can automatically be resolved using `build-dep`on Ubuntu.

    cd ~/snapd
    sudo apt-get build-dep .

Package build dependencies for other distributions can be found under the
`packages\`directory.

Go module dependencies are automatically resolved at build time.

### Building the snap with snapcraft

The easiest (though not the most efficient) way to test changes to snapd is to
build the snapd snap using _snapcraft_ and then install that snapd snap. The
snapcraft.yaml for the snapd snap is located at ./build-aux/snapcraft.yaml, and
can be built using snapcraft either in a LXD container or a multipass VM (or
natively with `--destructive-mode` on a Ubuntu 16.04 host).

Note: Currently, snapcraft's default track of 5.x does not support building the 
snapd snap, since the snapd snap uses `build-base: core`. Building with a 
`build-base` of core uses Ubuntu 16.04 as the base operating system (and thus 
root filesystem) for building and Ubuntu 16.04 is now in Extended Security 
Maintenance (ESM - see https://ubuntu.com/blog/ubuntu-16-04-lts-transitions-to-extended-security-maintenance-esm), and as such only is buildable using snapcraft's 4.x channel. At some point in the future,
the snapd snap should be moved to a newer `build-base`, but until then `4.x` 
needs to be used.

Install snapcraft from the 4.x channel:

```
sudo snap install snapcraft --channel=4.x
```

Then run snapcraft:

```
snapcraft
```

Now the snapd snap that was just built can be installed with:

```
snap install --dangerous snapd_*.snap
```

To go back to using snapd from the store instead of the custom version we 
installed (since it will not get updates as it was installed dangerously), you
can either use `snap revert snapd`, or you can refresh directly with 
`snap refresh snapd --stable --amend`.

Note: It is also sometimes useful to use snapcraft to build the snapd snap for
other architectures using the `remote-build` feature, however there is currently a
bug in snapcraft around using the `4.x` track and using `remote-build`, where the
Launchpad job created for the remote-build will attempt to use the `latest` track instead
of the `4.x` channel. This was recently fixed in snapcraft in https://github.com/snapcore/snapcraft/pull/3600
which for now requires using the `latest/edge` channel of snapcraft instead of the
`4.x` track which is needed to build the snap locally. However, this fix does 
then introduce a different problem due to one of the tests in the snapd git tree
which currently has circular symlinks in the tree, which due to a regression in
snapcraft is no longer usable with remote-build. Removing these files in the 
snapd git tree is tracked at https://bugs.launchpad.net/snapd/+bug/1948838, but
in the meantime in order to build remotely with snapcraft, do the following:

```
snap refresh snapcraft --channel=latest/edge
```

And then get rid of the symlinks in question:

```
rm -r tests/main/validate-container-failures/
```

Now you can use remote-build with snapcraft on the snapd tree for any desired 
architectures:

```
snapcraft remote-build --build-on=armhf,s390x,arm64
```

And to go back to building the snapd snap locally, just revert the channel back
to 4.x:

```
snap refresh snapcraft --channel=4.x/stable
```


#### Splicing the snapd snap into the core snap

Sometimes while developing you may need to build a version of the _core_ snap
with a custom snapd version. The snapcraft.yaml for the core snap currently is
complex in that it assumes it is built inside Launchpad with the 
`snappy-dev/image` PPA enabled, so it is difficult to inject a custom version of
snapd into this by rebuilding the core snap directly, so an easier way is to 
actually first build the snapd snap and inject the binaries from the snapd snap
into the core snap. This currently works since both the snapd snap and the core 
snap have the same `build-base` of Ubuntu 16.04. However, at some point in time
this trick will stop working when the snapd snap starts using a `build-base` other
than Ubuntu 16.04, but until then, you can use the following trick to more 
easily get a custom version of snapd inside a core snap.

First follow the steps above to build a full snapd snap. Then, extract the core
snap you wish to splice the custom snapd snap into:

```
sudo unsquashfs -d custom-core core_<rev>.snap
```

`sudo` is important as the core snap has special permissions on various 
directories and files that must be preserved as it is a boot base snap.

Now, extract the snapd snap, again with sudo because there are `suid` binaries
which must retain their permission bits:

```
sudo unsquashfs -d custom-snapd snapd-custom.snap
```

Now, copy the meta directory from the core snap outside to keep it and prevent
it from being lost when we replace the files from the snapd snap:

```
sudo cp ./custom-core/meta meta-core-backup
```

Then copy all the files from the snapd snap into the core snap, and delete the
meta directory so we don't use any of the meta files from the snapd snap:

```
sudo cp -r ./custom-snapd/* ./custom-core/
sudo rm -r ./custom-core/meta/
sudo cp ./meta-core-backup ./custom-core/
```

Now we can repack the core snap:

```
sudo snap pack custom-core
```

Sometimes it is helpful to modify the snap version in 
`./custom-core/meta/snap.yaml` before repacking with `snap pack` so it is easy
to identify which snap file is which.


### Building (natively)

To build the `snap` command line client:

```
cd ~/snapd
mkdir -p /tmp/build
go build -o /tmp/build/snap ./cmd/snap
```

To build the `snapd` REST API daemon:

```
cd ~/snapd
mkdir -p /tmp/build
go build -o /tmp/build/snapd ./cmd/snapd
```

To build all the`snapd` Go components:

```
cd ~/snapd
mkdir -p /tmp/build
go build -o /tmp/build ./...
```

### Cross-compiling (example: ARM v7 target)

Install a suitable cross-compiler for the target architecture.

```
sudo apt-get install gcc-arm-linux-gnueabihf
```

Verify the default architecture version of your GCC cross-compiler.

```
arm-linux-gnueabihf-gcc -v
:
--with-arch=armv7-a
--with-fpu=vfpv3-d16
--with-float=hard
--with-mode=thumb
```

Verify the supported Go cross-compile ARM targets [here](
https://github.com/golang/go/wiki/GoArm).

`Snapd` depends on libseccomp v2.3 or later. The following instructions can be
used to cross-compile the library:

```
cd ~/
git clone https://github.com/seccomp/libseccomp
cd libseccomp
./autogen.sh
./configure --host=arm-linux-gnueabihf --prefix=${HOME}/libseccomp/build
make && make install
```

Setup the Go environment for cross-compiling.

```
export CC=arm-linux-gnueabihf-gcc
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${HOME}/libseccomp/build/lib"
export GOOS=linux
export GOARCH=arm
export GOARM=7
```

The Go environment variables are now explicitly set to target the ARM v7
architecture.

Run the same build commands from the Building (natively) section above.

Verify the target architecture by looking at the application ELF header.

```
readelf -h /tmp/build/snapd
:
Class:                             ELF32
OS/ABI:                            UNIX - System V
Machine:                           ARM
```

CGO produced ELF binaries contain additional architecture attributes that
reflect the exact ARM architecture we targeted.

```
readelf -A /tmp/build/snap-seccomp
:
File Attributes
  Tag_CPU_name: "7-A"
  Tag_CPU_arch: v7
  Tag_FP_arch: VFPv3-D16
```
### Contributing

Contributions are always welcome!

Please make sure that you sign the Canonical contributor agreement [here](
http://www.ubuntu.com/legal/contributors).

Complete requirements can be found in CONTRIBUTING.md.

Contributions are submitted through a Pull Request created from a fork of the
`snapd` repository (under your GitHub account). 

Start by creating a fork of the `snapd` repository on GitHub.

Add your fork as an additional remote to an already cloned `snapd` main
repository. Replace `<user>` with your GitHub account username.

```
cd ~/snapd
git remote add fork git@github.com:<user>/snapd.git
```

Create a working branch on which to commit your work. Replace
`<branchname>` with a suitable branch name.

```
git checkout -b <branchname>
```

Make changes to the repository and commit the changes to your
working branch. Push the changes to your forked `snapd` repository.

```
git commit -a -m "commit message"
git push fork <branchname>
```

Create the Pull Request for your branch on GitHub.

This complete process is outlined in the GitHub documentation [here](
https://docs.github.com/en/github/collaborating-with-pull-requests).

We value good tests, so when you fix a bug or add a new feature we highly
encourage you to add tests.

For more information on testing, please see the Testing section.

### Testing

Install the following package(s) to satisfy test dependencies.

```
sudo apt-get install python3-yamlordereddictloader
```

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
    $ curl https://storage.googleapis.com/snapd-spread-tests/spread/spread-amd64.tar.gz | tar -xz -C $GOPATH/bin

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

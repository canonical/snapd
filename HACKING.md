# Hacking on snapd

Hacking on `snapd` is fun and straightforward. The code is extensively unit
tested and we use the [spread](https://github.com/snapcore/spread)
integration test framework for the integration/system level tests.

## Setting up

### Supported Ubuntu distributions

Ubuntu 18.04 LTS or later is recommended for `snapd` development.
Usually, the latest LTS would be the best choice.

>If you want to build or test on older versions of Ubuntu, additional steps
may be required when installing dependencies.


### Supported Go version

[Go 1.13](https://pkg.go.dev/go@go1.13) or later is required to build `snapd`.

>If you need to build older versions of snapd, please have a look at the file
[debian/control](debian/control) to find out what dependencies were needed at the time
(including which version of the Go compiler).

### Setting up build environment

`snapd` is published and maintained as a [Go module](https://go.dev/doc/modules/managing-source). 
A module is a collection of [Go packages](https://go.dev/ref/spec#Packages) 
stored in a file tree with a [go.mod](go.mod) file at its root. 
The [go.mod](go.mod) file defines the module’s _module path_, 
which is also the import path used for the root directory, 
and its _dependency requirements_. 
All this allows maintaining `snapd` outside of the strict hierarchy of `GOPATH`, 
that was mandatory before the release of [Go 1.13](https://pkg.go.dev/go@go1.13)
(see [build @Go1.10](https://pkg.go.dev/go/build@go1.10)).  

Even with Go modules mode, `GOPATH` is still used by the Go tool implicitly, 
to store downloaded source code and compiled commands 
(see [Go docs](https://pkg.go.dev/cmd/go#hdr-GOPATH_and_Modules)).

> If `$GOPATH` is not set explicitly, in Unix-like systems it defaults to `$HOME/go`.

One can easily find the location of the `GOPATH` file-tree on any enabled machine
by running the following command:

```
go env GOPATH
```

#### Backward compatibility

Some tools we use still rely on `$GOPATH/bin` being listed in `$PATH` 
(see [Running spread with QEMU](#running-spread-with-qemu)).
Add `$GOPATH/bin` to your `PATH` in your shell login script 
(e.g. `~/.bashrc` for bash):

```sh
export PATH=$PATH:$(go env GOPATH)/bin
```

If your `GOPATH` contains more than one path, you need to add `/bin`
suffix to each path:

```sh
export PATH=$PATH:$(go env GOPATH | sed -e "s|:|/bin:|g")/bin
```

#### Setting custom `GOPATH` (_Optional_)

Though it's no longer required, one still may set a custom location for `GOPATH`.

```sh
export GOPATH=${HOME}/work
```

This change can be done permanently either using Linux conventional way
(see the `$GOPATH/bin` [example](#backward-compatibility)) or using the Go tool:

```sh
go env -w GOPATH=${HOME}/work
```

### Getting snapd sources

The easiest way to get the source for `snapd` is to clone the GitHub repository
in a directory where you have read-write permissions, such as your home
directory.

    cd ~/
    git clone https://github.com/snapcore/snapd.git
    cd snapd

This will allow you to build and test `snapd`. If you wish to contribute to
the `snapd` project, please see the [Contributing section](#contributing).

### Installing build dependencies

Build dependencies can automatically be resolved using `build-dep`on Ubuntu.

    cd ~/snapd
    sudo apt-get build-dep .

Package build dependencies for other distributions can be found under the
[./packaging/](./packaging/) directory.

Go module dependencies are automatically resolved at build time.
One can do it directly, without building, using the following commands:

```
cd ~/snapd
go get ./...
```

## Building

### Building the snap with snapcraft <!-- TODO: (A) Is this section outdated??? -->

The easiest (though not the most efficient) way to test changes to snapd is to
build the snapd snap using _snapcraft_ and then install that snapd snap. The
[snapcraft.yaml](./build-aux/snapcraft.yaml) for the snapd snap is located at 
[./build-aux/](./build-aux/), and
can be built using snapcraft either in a LXD container or a multipass VM (or
natively with `--destructive-mode` on a Ubuntu 18.04 host).

Note: Currently, snapcraft's default track of 5.x does not support building the 
snapd snap, since the snapd snap uses `build-base: core`. Building with a 
`build-base` of core uses Ubuntu 16.04 as the base operating system (and thus 
root filesystem) for building and Ubuntu 16.04 is now in Extended Security 
Maintenance (ESM, see 
[Ubuntu 16.04 LTS ESM](https://ubuntu.com/blog/ubuntu-16-04-lts-transitions-to-extended-security-maintenance-esm)),
and as such only is buildable using snapcraft's 4.x channel. At some point in the future,
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
with a custom snapd version. 
The [snapcraft.yaml](https://github.com/snapcore/core20/blob/master/snapcraft.yaml) 
for the core snap currently is complex in that it assumes it is built inside Launchpad with the 
[ppa:snappy-dev/image](https://launchpad.net/~snappy-dev/+archive/ubuntu/image/) 
enabled, so it is difficult to inject a custom version of
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


### Building natively

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

### Building with cross-compilation (_example: ARM v7 target_)

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

`Snapd` depends on [libseccomp](https://github.com/seccomp/libseccomp#readme) 
v2.3 or later. The following instructions can be
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

```sh
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

## Contributing

Contributions are always welcome!

Please make sure that you sign the Canonical contributor agreement [here](
http://www.ubuntu.com/legal/contributors).

Complete requirements can be found in [CONTRIBUTING.md](./CONTRIBUTING.md).

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
local working branch.

```sh
git commit -sa -m "<feature/subject-prefix>: <brief-description-of-the-change>

<more-elaborate-description-with-reasoning-about-the-changed>"
``` 

>If it's justified, one might consider several commits before pushing 
their work to the remote repository. 
One should strive to have only one commit per topic, and to avoid pushing 
many topics in the same attempt.  

Push the changes to your forked `snapd` repository.

```
git push fork <local-branchname>[:<remote-branchname>]
```

>Note that if the `<remote-branchname>` is not specified, a new remote branch 
is created implicitly, by the name of the `<local-branchname>`.

Create the Pull Request for your branch on GitHub.

This complete process is outlined in the GitHub documentation [here](
https://docs.github.com/en/github/collaborating-with-pull-requests).

## Testing

### Running unit-tests

We value good tests, so when you fix a bug or add a new feature we highly
encourage you to add tests.

Install the following package(s) to satisfy test dependencies.

```
sudo apt-get install python3-yamlordereddictloader dbus-x11
```

To run the various tests that we have to ensure a high quality source just run:

    ./run-checks

This will check if the source format is consistent, that it builds, all tests
work as expected and that "go vet" has nothing to complain.

The source format follows the `gofmt -s` formating. Please run this on your 
source files if `run-checks` complains about the format.

You can run an individual test for a sub-package by changing into that 
directory and:

```
LANG=C.UTF-8 go test -check.f $testname
```

If a test hangs, you can enable verbose mode:

```
LANG=C.UTF-8 go test -v -check.vv
```

(or -check.v for less verbose output).

>Note `LANG=C.UTF-8` is specified in the examples above only to
ensure locale compatibility for some unit-tets. Some locales 
might have issues, e.g. `en_GB`.

There is more to read about the testing framework on the [website](https://labix.org/gocheck)

### Running integration tests

#### Downloading spread framework

To run the integration tests locally via QEMU, you need the latest version of
the [spread](https://github.com/snapcore/spread) framework. 
You can get spread, QEMU, and the build tools to build QEMU images with:

    $ sudo apt update && sudo apt install -y qemu-kvm autopkgtest
    $ curl https://storage.googleapis.com/snapd-spread-tests/spread/spread-amd64.tar.gz | tar -xz -C $(go env GOPATH)/bin

#### Building spread VM images

To run the spread tests via QEMU you need to create VM images in the
`~/.spread/qemu` directory:

    $ mkdir -p ~/.spread/qemu
    $ cd ~/.spread/qemu

Assuming you are building on Ubuntu 18.04 LTS ([Bionic Beaver](https://releases.ubuntu.com/18.04/)) 
(or a later Ubuntu), run the following to build a 64-bit Ubuntu 16.04 LTS 
(or later) with the correct `<release-name>` and `<release-version>`:

    $ autopkgtest-buildvm-ubuntu-cloud -r <release-name>
    $ mv autopkgtest-<release-name>-amd64.img ubuntu-<release-version>-64.img

Use the following table as an example reference:

| Release   | `<release-name>`  | `<release-version>`   |
| -------   | --------------    |-------------------    |
| [Xenial Xerus](https://releases.ubuntu.com/16.04/)  | xenial  | 16.04 |
| [Bionic Beaver](https://releases.ubuntu.com/18.04/) | beaver  | 18.04 |
| [Focal Fossa](https://releases.ubuntu.com/20.04/)   | focal   | 20.04 |


To build an Ubuntu 14.04 (Trusty Tahr) based VM, use:

    $ autopkgtest-buildvm-ubuntu-cloud -r trusty --post-command='sudo apt-get install -y --install-recommends linux-generic-lts-xenial && update-grub'
    $ mv autopkgtest-trusty-amd64.img ubuntu-14.04-64.img

> This is because we need at least 4.4+ kernel for snapd to run on Ubuntu 14.04 
LTS, which is available through the `linux-generic-lts-xenial` package.

If you are running Ubuntu 16.04 LTS, use 
`adt-buildvm-ubuntu-cloud` instead of `autopkgtest-buildvm-ubuntu-cloud` (the
latter replaced the former in 18.04):

    $ adt-buildvm-ubuntu-cloud -r xenial
    $ mv adt-<release-name>-amd64-cloud.img ubuntu-<release-version>-64.img

#### Downloading spread VM images

Alternatively, instead of building the QEMU images manually, you can download
pre-built and somewhat maintained images from 
[spread.zygoon.pl](spread.zygoon.pl). The images will need to be extracted 
with `gunzip` and placed into `~/.spread/qemu` as above.

#### Running spread with QEMU

Finally, you can run the spread tests for Ubuntu 18.04 LTS 64-bit with:

    $ spread -v qemu:ubuntu-18.04-64

>To run for a different system, replace `ubuntu-18.04-64` with a different system
name, which should be a basename of the [built](#building-spread-vm-images) or 
[downloaded](#downloading-spread-vm-images) Ubuntu image file.

For quick reuse you can use:

    $ spread -reuse qemu:ubuntu-18.04-64

It will print how to reuse the systems. Make sure to use
`export REUSE_PROJECT=1` in your environment too.

#### Running UC20 spread with QEMU

<!-- TODO: (B) Is it still working this way? -->

Ubuntu Core 20 on amd64 has a requirement to use UEFI, so there are a few 
additional steps needed to run spread with the `ubuntu-core-20-64` systems locally
using QEMU. For one, upstream spread currently does not support specifying what
kind of BIOS to use with the VM, so you have to build spread from this PR:
https://github.com/snapcore/spread/pull/95, and then use the environment 
variable `SPREAD_QEMU_BIOS` to specify an UEFI BIOS to use with the VM, for 
example the one from the OVMF package.  

>To get OVMF on Ubuntu, you can just install the `ovmf` package via `apt`.  

After installing OVMF, you can then run spread like so:

    $ SPREAD_QEMU_BIOS=/usr/share/OVMF/OVMF_CODE.fd spread -v qemu:ubuntu-core-20-64

This will enable testing UC20 with the spread, albeit without secure boot 
support. None of the native UC20 tests currently require secure boot however, 
all tests around secure boot are nested, see the section below about 
[running the nested tests](#running-nested-tests).

Also, due to the in-flux state of spread support for booting UEFI VM's like 
this, you can test `ubuntu-core-20-64` only by themselves and not with any other
system concurrently since the environment variable is global for all systems in
the spread run. This will be fixed in a future release of spread.

### Testing `snapd` daemon

To test the `snapd` REST API daemon on a snappy system you need to
transfer it to the snappy system and then run:

    sudo systemctl stop snapd.service snapd.socket
    sudo SNAPD_DEBUG=1 SNAPD_DEBUG_HTTP=3 ./snapd

To debug interaction with the snap store, you can set `SNAP_DEBUG_HTTP`.
It is a bitfield: dump requests: 1, dump responses: 2, dump bodies: 4.

> Note that in case you get some security profiles errors, when trying to install or refresh a snap, 
maybe you need to replace system installed snap-seccomp with the one aligned to the snapd that 
you are testing. To do this, simply backup `/usr/lib/snapd/snap-seccomp` and overwrite it with 
the testing one. Don't forget to rollback to the original when finish testing

### Running nested tests

Nested tests are used to validate features that cannot be tested with the regular tests.

The nested test suites work different from the other test suites in snapd. In this case each test runs in a new image
which is created following the rules defined for the test.

The nested tests are executed using [spread framework](#downloading-spread-framework). 
See the following examples using the QEMU and Google backends.

- _QEMU_: `spread qemu-nested:ubuntu-20.04-64:tests/nested/core20/tpm`  
- _Google_: `spread google-nested:ubuntu-20.04-64:tests/nested/core20/tpm`  

The nested system in all the cases is selected based on the host system. The following lines 
show the relation between host and nested `systemd` (same applies to the classic nested tests):

- ubuntu-16.04-64 => ubuntu-core-16-64
- ubuntu-18.04-64 => ubuntu-core-18-64
- ubuntu-20.04-64 => ubuntu-core-20-64

The tools used for creating and hosting the nested VMs are:

- _ubuntu-image snap_ is used to build the images
- _QEMU_ is used for the virtualization (with [_KVM_](https://www.linux-kvm.org/page/Main_Page) acceleration)

Nested test suite is composed by the following 4 suites:

- _classic_: the nested suite contains an image of a classic system downloaded from cloud-images.ubuntu.com 
- _core_: it tests a core nested system, and the images are generated with _ubuntu-image snap_
- _core20_: this is similar to the _core_ suite, but these tests are focused on UC20
- _manual_: tests on this suite create a non generic image with specific conditions

The nested suites use some environment variables to configure the suite 
and the tests inside it. The most important ones are described bellow:

- `NESTED_WORK_DIR`: it is path to the directory where all the nested assets and images are stored
- `NESTED_TYPE`: use core for Ubuntu Core nested systems or classic instead.
- `NESTED_CORE_CHANNEL`: the images are created using _ubuntu-image snap_, use it to define the default branch
- `NESTED_CORE_REFRESH_CHANNEL`: the images can be refreshed to a specific channel; use it to specify the channel
- `NESTED_USE_CLOUD_INIT`: use cloud init to make initial system configuration instead of user assertion
- `NESTED_ENABLE_KVM`: enable KVM in the QEMU command line
- `NESTED_ENABLE_TPM`: re-boot in the nested VM in case it is supported (just supported on UC20)
- `NESTED_ENABLE_SECURE_BOOT`: enable secure boot in the nested VM in a case it is supported (supported just on UC20)
- `NESTED_BUILD_SNAPD_FROM_CURRENT`: build and use either core or `snapd` from the current branch
- `NESTED_CUSTOM_IMAGE_URL`: download and use an custom image from this URL


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
target, available in [cmd/snap-confine/](cmd/snap-confine/) handy. It installs the locally built
version on your system and reloads the [AppArmor](https://apparmor.net/) profile.

>Note, the above configure options assume you are on Ubuntu and are generally
necessary to run/test graphical applications with your local version of
snap-confine. The `--with-host-arch-triplet` option sets your specific 
architecture and `--enable-nvidia-multiarch` allows the host's graphics drivers
and libraries to be shared with snaps. If you are on a distro other than
Ubuntu, try `--enable-nvidia-biarch` (though you'll likely need to add further
system-specific options too).

## Submitting patches

Please run `(cd cmd; make fmt)` before sending your patches for the "C" part of
the source code.

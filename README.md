[![Snapcraft](https://avatars2.githubusercontent.com/u/19532717?s=200)](https://snapcraft.io)

# Welcome to snapd

This is the code repository for **snapd**, the background service that manages
and maintains installed snaps.

Snaps are app packages for desktop, cloud and IoT that update automatically,
are easy to install, secure, cross-platform and dependency-free. They're being
used on millions of Linux systems every day.

Alongside its various service and management functions, snapd:
- provides the _snap_ command that's used to install and remove snaps and
  interact with the wider snap ecosystem
- implements the confinement policies that isolate snaps from the base system
  and from each other
- governs the interfaces that allow snaps to access specific system resources
  outside of their confinement

For general details, including
[installation](https://snapcraft.io/docs/installing-snapd) and [Getting
started](https://snapcraft.io/docs/getting-started) guides, head over to our
[Snap documentation](https://snapcraft.io/docs). If you're looking for
something to install, such as [Spotify](https://snapcraft.io/spotify) or
[Visual Studio Code](https://snapcraft.io/code), take a look at the [Snap
Store](https://snapcraft.io/store). And if you want to build your own snaps,
start with our [Creating a snap](https://snapcraft.io/docs/creating-a-snap)
documentation.

## Get involved

This is an [open source](COPYING) project and we warmly welcome community
contributions, suggestions, and constructive feedback. If you're interested in
contributing, please take a look at our [Code of Conduct](CODE_OF_CONDUCT.md)
first.

- to report an issue, please file [a bug
  report](https://bugs.launchpad.net/snapd/+filebug) on our [Launchpad issue
tracker](https://bugs.launchpad.net/snapd/)
- for suggestions and constructive feedback, create a post on the [Snapcraft
  forum](https://forum.snapcraft.io/c/snapd)
- to build snapd manually, or to get started with snapd development, see
  [HACKING.md](HACKING.md)

## Get in touch

We're friendly! We have a community forum at
[https://forum.snapcraft.io](https://forum.snapcraft.io) where we discuss
feature plans, development news, issues, updates and troubleshooting. You can
chat in realtime with the snapd team and our wider community on the
[#snappy](https://web.libera.chat?channel=#snappy) IRC channel on
[libera chat](https://libera.chat/).

For news and updates, follow us on [Twitter](https://twitter.com/snapcraftio)
and on [Facebook](https://www.facebook.com/snapcraftio).

## Project status

| Service | Status |
|-----|:---|
| [Github Actions](https://github.com/actions/) |  [![Build Status][actions-image]][actions-url]  |
| [GoReport](https://goreportcard.com/) |  [![Go Report Card][goreportcard-image]][goreportcard-url] |
| [Codecov](https://codecov.io/) |  [![codecov][codecov-image]][codecov-url] |

[actions-image]: https://github.com/snapcore/snapd/actions/workflows/test.yaml/badge.svg?branch=master
[actions-url]: https://github.com/snapcore/snapd/actions?query=branch%3Amaster+event%3Apush

[goreportcard-image]: https://goreportcard.com/badge/github.com/snapcore/snapd
[goreportcard-url]: https://goreportcard.com/report/github.com/snapcore/snapd

[codecov-url]: https://codecov.io/gh/snapcore/snapd
[codecov-image]: https://codecov.io/gh/snapcore/snapd/branch/master/graph/badge.svg

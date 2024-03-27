[![Snapcraft](https://avatars2.githubusercontent.com/u/19532717?s=200)](https://snapcraft.io)

# Welcome to snapd-testing-tools

This is the code repository for **snapd-testing-tools**, the set of tools
used by snapd for testing porpuses.

The tools in this project are designed and tested independently, making them easy
to be imported and used by any project.

## Get involved

This is an [open source](COPYING) project and we warmly welcome community
contributions, suggestions, and constructive feedback. If you're interested in
contributing, please take a look at our [Code of Conduct](CODE_OF_CONDUCT.md)
first.

- to report an issue, please file [a bug
  report](https://bugs.launchpad.net/snappy/+filebug) on our [Launchpad issue
tracker](https://bugs.launchpad.net/snappy/)
- for suggestions and constructive feedback, create a post on the [Snapcraft
  forum](https://forum.snapcraft.io/c/snapd)

## Get in touch

We're friendly! We have a community forum at
[https://forum.snapcraft.io](https://forum.snapcraft.io) where we discuss
feature plans, development news, issues, updates and troubleshooting. You can
chat in realtime with the snapd team and our wider community on the
[#snappy](https://web.libera.chat?channel=#snappy) IRC channel on
[libera chat](https://libera.chat/).

For news and updates, follow us on [Twitter](https://twitter.com/snapcraftio)
and on [Facebook](https://www.facebook.com/snapcraftio).

## Adding new tools

The tools included in this project are intended to be reused by other projects.

Tools are supported in all the systems included in spread.yaml file.

Read the following considerations before adding new tools:

 - Each tool needs to be accompanied by at least 1 spread test in `tests/<tool-name>/`
 - At least 1 spread test needs to be included in the tests directory for each tool
 - If the tool is a shell script, it needs to first pass a [ShellCheck](https://github.com/koalaman/shellcheck) assessment
 - All tools need to be as generic as possible
 - Each tool must also provide a command line interface (CLI), including _help_ output

## Adding new utils

The utils included in this project are intended to be reused by other projects.

Utils are used as a complement for spread tests executions. Those are not intended to be used by spread tests.
For example utils are used on github action workflows to analyze tests code and outputs.

Utils are supported in ubuntu-18.04 and higher.

Read the following considerations before adding new utils:

 - Each util needs to be accompanied by at least 1 spread test in `tests/<util-name>/`
 - At least 1 spread test needs to be included in the tests directory for each util
 - If the util is a shell script, it needs to first pass a [ShellCheck](https://github.com/koalaman/shellcheck) assessment
 - All utils need to be as generic as possible
 - Each util must also provide a command line interface (CLI), including _help_ output


## Project status

| Service | Status |
|-----|:---|
| [Github Actions](https://github.com/actions/) |  ![Build Status][actions-image]  |

[actions-image]: https://github.com/snapcore/snapd-testing-tools/actions

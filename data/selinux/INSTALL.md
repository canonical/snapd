# Installing the policy


## Building the policy module

There's only a few requirements for building the policy module:

* GNU make (Fedora / Debian: `make`)
* SELinux policy development package (Fedora: `selinux-policy-devel` / Debian: `selinux-policy-dev`)

Install the packages as appropriate, then run `make`.

For example, on Fedora:

```bash
$ sudo dnf install make selinux-policy-devel
$ make
```

## Install the module to the system

To install the module, run the following as root:

```bash
$ semodule -i snappy.pp
```

When shipping this module in a distribution package, use the following command instead:

```bash
$ semodule -X 200 -i snappy.pp
```

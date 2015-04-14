# OEM snappy package

The `oem` snappy package is a snappy package `type` that is used to setup and
personalize the system according to an OEM.

It covers a broad range, such as the software stack with it’s configuration and
hardware enablement.

There can only be *one* snappy package of `type` `oem` and it can only be
installed during image provision.

## Nomenclature

Some parts of this text refer to pure snappy packages, and `device` or
`ubuntu-core` packages, the intent is that in the future, `device` and
`ubuntu-core` would migrate to being pure snappy packages in the future. This
has been generalized in some text with the concept of `parts`, in this writing
everything is a *package*.

## Customization entry points

### Default packages

The `oem` snap can provide a set of default packages to be installed during
either provisioning or first boot. The former is interesting to IoT scenarios
while the latter is useful for cloud deployments (although `cloud-init`
directly also serves the purpose for pure clouds).

In the case of first boot provisioning, the data is just fed into cloud-init for
the appropriate actions to take place.

For the preinstalled case, the provisioning tool will download the package
selection from the store and provision onto the system.

### Snappy package configuration

Each snappy package can be configured independently or by feeding a full
configuration with all packages.

The `oem` package shall initially support providing a `config.yaml` describing
each package that is to be configured.

On first boot of the system, this `config.yaml` file will be processed and the
described configuration will be applied. Note: factory resetting the system
will create a first boot scenario and therefore `config.yaml` will be
processed.

Some configuration entries are currently driven entirely by `cloud-init`,
these are fine for a cloud enabled instance of snappy, not so much for IoT.
The intent of the `ubuntu-core` package configuration is to wrap around
`cloud-init` and use it where possible and relevant.

### Store ID

If a non-default store is required, one may use the `store/id` entry and
`snappy` will use it to reach the appropriate store.

### Branding

Branding can be set in the form of a slogan and an image. `snappy` and it’s
`webdm` counterpart will use this information to brand the system accordingly.

### Hardware

#### dtb

The default dtb (device tree blob) can be overridden by a key entry point in
the `package.yaml` for the `oem`.

If a dtb is specified during provisioning, it will be selected as the dtb to
use for the system. If using an AB partition layout, when an update for the
`oem` package is installed which updates the `dtb`, the update will be
installed to the *other* partition and a reboot will be requested.

An upgrade path must be calculated by `snappy` to determine the priority and
ordering for an `ubuntu-core` update and an `oem` snap update.

#### Bootloaders

Since each system boots differently, assets that are currently provided in
`flashassets` of the device package can be provided in the `oem` snap instead.
This is useful for systems that use the default `device` package (ie, one which
uses the officially supported Ubuntu kernel and initrd).

Examples of assets that may be provided via the `oem` snap are `MLO`, `u-boot`,
`UEnv.txt` `script.bin` or anything external to the system that allows for a
system to boot.

While these assets are typically used during provisioning, they may also be
used against a running system. Caution: updating these assets on a running
system may lead to a broken system unless redundancy or fallback machanisms
aren't provided by the OEM.

#### Partition layout

In the current layout, the `device` package contains a file called
`hardware.yaml`, the `partition-layout` will be migrated to the `oem` snappy 
package to provide a more generic `device` package.

The only supported layout today is AB.

## Structure and layout

The `package.yaml` is structured as

```yaml
name: package-string # mandatory
vendor: vendor-string # mandatory
icon: icon-path # mandatory
version: version-string # mandatory
type: oem # mandatory

config: # optional
    snappy-package-string:
        property-string: property-value

immutable-config: # optional
    - filter-string

oem:
    store: # optional
        id: id-string # optional

    branding: # optional
        name:  branding-name-string # optional
        logo: logo-path # optional

    software: # optional
        built-in:
            - # package list
        preinstalled:
            - # package list
    hardware: # mandatory
        platform: platform-string # mandatory
        architecture: architecture-string # mandatory (armhf, amd64, i386, arm64, ...)
        partition-layout: partition-layout-string # mandatory (system-AB)
        booloader: bootloader-string # mandatory (u-boot or grub)
        boot-assets: # optional
            files: #optional
                - path: file-path
            raw-files: #optional
                - path: file-path
                  offset: offset-uint64
```

The package header section is common to all packages

The general rules for config:

- only applied on first boot.
- if the config is immmutable, updates on in `oem` package will be reflected.

Rules about packages in the config:

- a package listed in this map is preinstalled on image roll out (`ubuntu-core`
  is implicit)

The `oem` part of the `package.yaml` is not a configuration per se and treated
separately.

Rules about `software`:

- `built-in` is a list of packages that cannot be removed.
- `preinstalled` is a list of packages that are installed but can be removed.

As an example

```yaml
name: beagleboneblack.sergiusens
vendor: Sergio Schvezov <sergiusens@gmail.com>
icon: meta/element14.png
version: 1.1
type: oem

config:
    ubuntu-core:
        hostname: myhostname
        services:
            - name: ssh
              enabled: true
        no-cloud: true
    config-example.canonical:
        msg: Yay!

immutable-config:
    - ubuntu-core/services/*
    - webdm/*

oem:
    store:
        id: mystore
    branding:
        name:  Beagle Bone Black
        logo: logo.png
    software:
        built-in:
            - webdm
        preinstalled:
            - system-status.victor
            - pastebinit.mvo
            - config-example.canonical
    hardware:
        platform: am335x-boneblack
        architecture: armhf
        partition-layout: system-AB
        bootloader: u-boot
        boot-assets:
            files:
                - path: uEnv.txt
            raw-files:
                - path: MLO
                  offset: 131072 # 128 * 1024
                - path: u-boot.img
                  offset: 393216 # 384 * 1024
```

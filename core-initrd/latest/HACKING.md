# Hacking the Initrd for Ubuntu Core

This document explains how to modify & update initrd for Ubuntu Core

# Purpose

Sometimes you may need to alter the behaviour of stock initrd or you may want to add a new functionality to it. 

# Target platforms

Modifying initrd is relatively easy for arm SBC's (Like RPi) or amd64 devices ( Like Intel NUCs ) without secure boot ( No TPM or PTT or similar)

For devices with secure boot, one need to :

TODO: Explain how to modify initrd when there is Secure boot / FDE.

# Prequisities

- A Linux Computer with various development tools
  - Latest Ubuntu Desktop is recommended
  - (For a SBC like RPi) you will need UART TTL serial debug cable (Because you will not have SSH in initrd)
  - initramfs-tools
  - snapcraft
  - etc

# Testing with spread

## Prerequisites for testing

You need to have the following software installed before you can test with spread
 - Go (https://golang.org/doc/install or ```sudo snap install go```)
 - Spread (install from source as per below)

## Installing spread

You can install spread by simply using ```snap install spread```, however this does not allow for the lxd-backend to be used.
To use the lxd backend you need to install spread from source, as the LXD profile support has not been upstreamed yet.
This document will be updated with the upstream version when the PR https://github.com/snapcore/spread/pull/136 merges. To install spread from source you need to do the following.

```
git clone https://github.com/Meulengracht/spread
cd spread
cd cmd/spread
go build .
go install .
```

## QEmu backend

1. Install the dependencies required for the qemu emulation
```
sudo apt update && sudo apt install -y qemu-kvm autopkgtest
```
2. Create a suitable ubuntu test image (noble) in the following directory where spread locates images. Note that the location is different when using spread installed through snap.
```
mkdir -p ~/.spread/qemu # This location is different if you installed spread from snap
cd ~/.spread/qemu
autopkgtest-buildvm-ubuntu-cloud -r noble
```
3. Rename the newly built image as the name will not match what spread is expecting
```
mv autopkgtest-noble-amd64.img ubuntu-24.04-64.img
```
4. Now you are ready to run spread tests with the qemu backend
```
cd ~/core-initrd # or wherever you checked out this repository
spread qemu-nested
```

## LXD backend
The LXD backend is the preffered way of testing locally as it uses virtualization and thus runs a lot quicker than
the qemu backend. This is because the container can use all the resources of the host, and we can support
qemu-kvm acceleration in the container for the nested instance.

This backend requires that your host machine supports KVM.

1. Setup any prerequisites and build the LXD image needed for testing. The following commands will install lxd
and yq (needed for yaml manipulation), download the newest image and import it into LXD.
```
sudo snap install lxd
sudo snap install yq
curl -o lxd-initrd-img.tar.gz https://storage.googleapis.com/snapd-spread-core/lxd/lxd-spread-initrd26-img.tar.gz
lxc image import lxd-initrd-img.tar.gz --alias ucspread26
lxc image show ucspread26 > temp.profile
yq e '.properties.aliases = "ucspread26,amd64"' -i ./temp.profile
yq e '.properties.remote = "images"' -i ./temp.profile
cat ./temp.profile | lxc image edit ucspread26
rm ./temp.profile ./lxd-initrd-img.tar.gz
```
2. Import the LXD coreinitrd test profile. Make sure your working directory is the root of this repository.
```
lxc profile create coreinitrd
cat tests/spread/core-initrd.lxdprofile | lxc profile edit coreinitrd
```
3. Set environment variable to enable KVM acceleration for the nested qemu instance
```
export SPREAD_ENABLE_KVM=true
```
4. Now you can run the spread tests using the LXD backend
```
spread lxd-nested
```

# Debugging

## Getting a debug shell in initrd

Getting a debug shell in initrd is very simple:
1. Boot your UC24 image on your RPi
2. Access to it via SSH
3. Edit your kernel commandline:

First your current kernel commandline is:
```
 ~$ cat /run/mnt/ubuntu-seed/cmdline.txt 
dwc_otg.lpm_enable=0 console=serial0,115200 elevator=deadline rng_core.default_quality=700 vt.handoff=2 quiet splash 
 ~$ 
```
Now you need to add following three arguments to it:
```
rd.systemd.debug_shell=serial0 dangerous rd.systemd.unit=emergency.service 
```

So it will look like:
```
 ~$ cat /run/mnt/ubuntu-seed/cmdline.txt 
dwc_otg.lpm_enable=0 console=serial0,115200 elevator=deadline rng_core.default_quality=700 vt.handoff=2 quiet splash rd.systemd.debug_shell=serial0 dangerous rd.systemd.unit=emergency.service
 ~$
```

**Warning**: Because of a [bug](https://bugs.launchpad.net/ubuntu/+source/flash-kernel/+bug/1933093) in boot.scr, following will not happen, and your RPi will boot to main rootfs again (you can ssh to it). Leaving this warning until this bug is fixed.

Finally, after a reboot, you will get a shell like this:
```
PM_RSTS: 0x00001000
....lot of logs here
Starting start4.elf @ 0xfec00200 partition 0

U-Boot 2020.10+dfsg-1ubuntu0~20.04.2 (Jan 08 2021 - 13:03:11 +0000)

DRAM:  3.9 GiB
....lot of logs
Decompressing kernel...
Uncompressed size: 23843328 = 0x16BD200
20600685 bytes read in 1504 ms (13.1 MiB/s)
Booting Ubuntu (with booti) from mmc 0:...
## Flattened Device Tree blob at 02600000
   Booting using the fdt blob at 0x2600000
   Using Device Tree in place at 0000000002600000, end 000000000260f07f

Starting kernel ...
[    1.224420] spi-bcm2835 fe204000.spi: could not get clk: -517

BusyBox v1.30.1 (Ubuntu 1:1.30.1-4ubuntu6.3) built-in shell (ash)
Enter 'help' for a list of built-in commands.
#
```

In this state, if you want to pivot-root to main rootfs, just execute:

```
# systemctl start basic.target
```

# Hacking without re-building

Sometimes, in order for testing some new feature, rebuilding is not necessary. It is possible to add your fancy nice application to current initrd.img. Depending on the board, we need to do different things, so we will show how to do this for an RPi and for x86 images.

## Hacking an RPi initrd

Basically:
- Download current initrd.img from your RPi to your host
- unpack it to a directory
- add your binary / systemd unit into it
- repack initrd.img
- upload to your RPi
- reboot

```
 ~ $  mkdir uc-initrd
 ~ $  cd uc-initrd/
 ~/uc-initrd $  scp pi:/run/mnt/ubuntu-boot/uboot/ubuntu/pi-kernel_292.snap/initrd.img .
initrd.img                                                            100%   20MB  34.8MB/s   00:00    
 ~/uc-initrd $  unmkinitramfs initrd.img somedir
 ~/uc-initrd $  tree -L 1 somedir/
somedir/
├── bin -> usr/bin
├── etc
├── init -> usr/lib/systemd/systemd
├── lib -> usr/lib
├── lib64 -> usr/lib64
├── sbin -> usr/bin
└── usr

5 directories, 2 files
 ~/uc-initrd $  echo "echo \"Hello world\"" > somedir/usr/bin/hello.sh
 ~/uc-initrd $  chmod +x somedir/usr/bin/hello.sh 
 ~/uc-initrd $  somedir/usr/bin/hello.sh 
Hello world
 ~/uc-initrd $  
 ~/uc-initrd $  cd somedir/
 ~/uc-initrd/somedir $  find ./ | cpio --create --quiet --format=newc --owner=0:0 | lz4 -9 -l > ../initrd.img.new
103890 blocks
 ~/uc-initrd/somedir $  cd ..
 ~/uc-initrd $  file initrd.img
initrd.img: LZ4 compressed data (v0.1-v0.9)
 ~/uc-initrd $  file initrd.img.new 
initrd.img.new: LZ4 compressed data (v0.1-v0.9)
 ~/uc-initrd $  ll
total 40252
drwxrwxr-x  3       4096 Haz 21 17:53 ./
drwxr-x--- 36       4096 Haz 21 17:52 ../
-rwxr-xr-x  1   20600685 Haz 21 17:43 initrd.img*
-rw-rw-r--  1   20601051 Haz 21 17:53 initrd.img.new
drwxrwxr-x  4       4096 Haz 21 17:49 somedir/
 ~/uc-initrd $  scp initrd.img.new pi:/tmp/
initrd.img.new                                                        100%   20MB  41.6MB/s   00:00    
 ~/uc-initrd $  ssh pi
Welcome to Ubuntu 20.04.2 LTS (GNU/Linux 5.4.0-1037-raspi aarch64)
......
Last login: Mon Jun 21 14:40:10 2021 from 192.168.1.247
 ~$ sudo cp /tmp/initrd.img.new /run/mnt/ubuntu-boot/uboot/ubuntu/pi-kernel_292.snap/initrd.img 
 ~$ sync
 ~$ sudo reboot
Connection to 192.168.1.224 closed by remote host.
Connection to 192.168.1.224 closed.
 ~/uc-initrd $ 
```

And then, finally, on your serial console :
```
Starting kernel ...

[    1.223730] spi-bcm2835 fe204000.spi: could not get clk: -517


BusyBox v1.30.1 (Ubuntu 1:1.30.1-4ubuntu6.3) built-in shell (ash)
Enter 'help' for a list of built-in commands.

# hello.sh 
Hello world
# 

```

## Hacking generic x86 initrd

For x86 the process is a bit different as in this case the initrd is a
section in a PE+/COFF binary. We can download the snap and extract the
initrd with:

```
$ snap download --channel=20/stable pc-kernel
$ unsquashfs -d pc-kernel pc-kernel_*.snap
$ objcopy -O binary -j .initrd pc-kernel/kernel.efi initrd.img
$ unmkinitramfs initrd.img initrd
```

Then, you can change the initrd as described in the RPi section. After
that, to repack everything, run these commands:


```
$ cd initrd
$ find . | cpio --create --quiet --format=newc --owner=0:0 | lz4 -l -7 > ../initrd.img
$ cd -
$ apt install systemd-boot-efi systemd-ukify
$ objcopy -O binary -j .linux pc-kernel/kernel.efi linux
$ /usr/lib/systemd/ukify build --linux linux \
          --initrd initrd.img \
          --output pc-kernel/kernel.efi
$ snap pack pc-kernel
```

Note that the systemd-boot-efi package should match the Ubuntu release
of the kernel being modified (both version and architecture). You can
use this new kernel snap while building image, or copy it over to your
device and install. The new `kernel.efi` won't be signed, so Secure
Boot will not be possible anymore, unless signed again with a key
accepted by the system.

# Hacking with rebuilding

The initrd is part of the kernel snap, so ideally we would prefer to
simply build it by using snapcraft. However, the snapcraft recipe for
the kernel downloads binaries from already built kernel packages, so
we cannot use it easily for local hacking. So we will provide
instructions on how to build the initrd from scratch and insert it in
the kernel snap.

First, we need to build the debian package, for this you can use
debuild or dpkg-buildpackage from the root folder:

```
$ sudo apt build-dep ./
$ debuild -us -uc
```

Then, install the package in the container:

```
$ sudo apt install ../ubuntu-core-initramfs_*.deb
```

We extract the kernel from the pc-kernel snap:

```
$ snap download --channel=20/stable pc-kernel
$ unsquashfs -d pc-kernel pc-kernel_*.snap
```

We can now extract the kernel image from kernel.efi:

```
$ snap info pc-kernel | grep 20/stable
  20/stable:        5.4.0-87.98.1  2021-09-30 (838) 295MB -
$ kernelver=5.4.0-87-generic
$ objcopy -O binary -j .linux pc-kernel/kernel.efi kernel.img-"${kernelver}"
```

We can inject it in the kernel snap as in previous sections. It is
also possible to force the build of `kernel.efi` (or just the initrd), with:

```
$ ubuntu-core-initramfs create-initrd --kernelver=$kernelver --kerneldir pc-kernel/modules/${kernelver} --firmwaredir pc-kernel/firmware --output initrd.img
$ ubuntu-core-initramfs create-efi --kernelver=$kernelver --initrd initrd.img --kernel kernel.img --output kernel.efi
```

Note that for RPi we need only the initrd image file.

Now `kernel.efi-5.4.0-87-generic` has been created with the new initramfs. We can put it back into the snap:

```
$ cp kernel.efi-$kernelver pc-kernel/kernel.efi
$ snap pack pc-kernel
```

# Troubleshooting


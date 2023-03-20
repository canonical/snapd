# Test gadget snap with pre-installed snaps

1. Branch snappy-systems
2. Modify the `snap.yaml` to add a snap, e.g.:

    ```diff
    === modified file 'generic-amd64/meta/snap.yaml'
    --- generic-amd64/meta/snap.yaml	2015-07-03 12:50:03 +0000
    +++ generic-amd64/meta/snap.yaml	2015-11-09 16:26:12 +0000
    @@ -7,6 +7,8 @@
     config:
         ubuntu-core:
             autopilot: true
    +    config-example-bash:
    +      msg: "huzzah\n"

     gadget:
         branding:
    @@ -20,3 +22,7 @@
             boot-assets:
                 files:
                     - path: grub.cfg
    +
    +    software:
    +      built-in:
    +        - config-example-bash.canonical
    ```

  (for amd64, or modify for other arch).

3. Build the gadget snap.
4. Create an image using the gadget snap.
5. Boot the image
6. Run:

        sudo journalctl -u snapd.firstboot.service

    * Check that it shows no errors.


7. Run:

        config-example-bash.hello

    * Check that it prints `huzzah`.

# Test gadget snap with modules

1. Branch snappy-systems
2. Modify the `snap.yaml` to add a module, e.g.:

    ```diff
    === modified file 'generic-amd64/meta/snap.yaml'
    --- generic-amd64/meta/snap.yaml	2015-07-03 12:50:03 +0000
    +++ generic-amd64/meta/snap.yaml	2015-11-12 10:14:30 +0000
    @@ -7,6 +7,7 @@
     config:
         ubuntu-core:
             autopilot: true
    +        load-kernel-modules: [tea]

     gadget:
         branding:

    ```

3. Build the gadget snap.
4. Create an image using the gadget snap.
5. Boot the image.
6. Run:

        sudo journalctl -u snapd.firstboot.service

    * Check that it shows no errors.


7. Check that the output of `lsmod` includes the module you requested. With the above example,

        lsmod | grep tea

# Test resize of writable partition

1. Get the start of the *writable* partition:

        parted /path/to/ubuntu-snappy.img unit b print

    * Note down the number of bytes in the *Start* column for the *writable* partition.

2. Make a loopback block device for the writable partition, replacing *{start}* with the number
   from the previous step:

        sudo losetup -f --show -o {start} /path/to/ubuntu-snappy.img

    * Note down the loop device.

3. Shrink the file system to the minimum, replacing *{dev}* with the device from the previous
   step:

        sudo e2fsck -f {dev}
        sudo resize2fs -M {dev}

4. Delete the loopback block device:

        sudo losetup -d {dev}

5. Get the end of the *writable* partition:

        parted /path/to/ubuntu-snappy.img unit b print

    * Note down the *Number* of the *writable* partition and the number of bytes in the *End*
      column.

6. Resize the *writable* partition, using the partition *{number}* from the last step, and
   replacing the *{end}* with a value that leaves more than 10% space free at the end.

        parted /path/to/ubuntu-snappy.img unit b resizepart {number} {end*85%}

7. Boot the image.

8. Print the free space of the file system, replacing *{dev}* with the device that has the *writable* partition:

        sudo parted -s {dev} unit % print free

    * Check that the writable partition was resized to occupy all the empty space.

# Test Mir interface by running Mir kiosk snap examples

1. Install Virtual Machine Manager
2. Stitch together a new image
3. Build both the mir-server and the mir-client snaps from lp:~mir-team/+junk/mir-server-snap and lp:~mir-team/+junk/snapcraft-mir-client
4. Copy over the snaps and sideload install the mir-server snap, which should result in a mir-server launching black blank screen with a mouse available.
5. Now install the mir-client snap.
6. Manually connect mir-client:mir to mir-server:mir due to bug 1577897, then start the mir-client service manually.
7. This should result in the Qt clock example app being displayed.

# Test serial-port interface using miniterm app

1. Using Ubuntu classic build and install a simple snap containing the Python
   pySerial module. Define a app that runs the module and starts miniterm.

```yaml
  name: miniterm
  version: 1
  summary: pySerial miniterm in a snap
  description: |
      Simple snap that contains the modules necessary to run
      pySerial. Useful for testing serial ports.
  confinement: strict
  apps:
      open:
        command: python3 -m serial.tools.miniterm
        plugs: [serial-port]
  parts:
      my-part:
        plugin: nil
        stage-packages:
          - python3-serial
```

2. Ensure the 'serial-port' interface is connected to miniterm
3. Use sudo miniterm.open /dev/tty<DEV> to open a serial port

# Test pulseaudio interface using paplay, pactl

1. Using a Snappy core image on a device like an RPi2/3, install the
   build and install the simple-pulseaudio snap from the following
   git repo:
   git://git.launchpad.net/~snappy-hwe-team/snappy-hwe-snaps/+git/examples
2. $ cd examples/simple-pulseaudio
3. Ensure that the 'pulseaudio' interface is connected to paplay
   $ sudo snap connect simple-pulseaudio:pulseaudio
   $ sudo snap connections simple-pulseaudio
4. Use /snap/bin/simple-pulseaudio.pactl stat and verify that you see
   valid output status from pulseaudio
5. Use /snap/bin/simple-pulseaudio.paplay $SNAP/usr/share/sounds/alsa/Noise.wav and verify
   that you can hear the sound playing

# Test bluetooth-control interface

1. Using Ubuntu classic build and install the bluetooth-tests snap
   from the store.

2. Stop system BlueZ service

$ sudo systemctl stop bluetooth

or if you have the bluez snap installed

$ snap remove bluez

3. Run one of the tests provided by the bluetooth-tests snap

 $ sudo /snap/bin/bluetooth-tests.hci-tester

   and verify it actually passes. If some of the tests fail
   there will be a problem with the particular kernel used on
   the device.

# Test tpm interface with tpm-tools

1. Install tpm snap from store.
2. Connect plug tpm:tpm to slot ubuntu-core:tpm.
3. Reboot the system so daemon in tpm snap can get proper permissions.
4. Use tpm.version to read from tpm device and make sure it shows no error.

        $ tpm.version
        xKV  TPM 1.2 Version Info:
          Chip Version:        1.2.5.81
          Spec Level:          2
          Errata Revision:     3
          TPM Vendor ID:       WEC
          Vendor Specific data: 0000
          TPM Version:         01010000
          Manufacturer Info:   57454300

# Test fwupd interface with uefi-fw-tools

1. Ensure your BIOS support UEFI firmware upgrading via UEFI capsule format
2. Install the uefi-fw-tools snap from the store
3. Ensure the 'fwupd' interface is connected

 $ sudo snap connect uefi-fw-tools:fwupdmgr uefi-fw-tools:fwupd

4. Check if the device support UEFI firmware updates

 $ sudo uefi-fw-tools.fwupdmgr get-devices

5. Get available UEFI firmware from the server

 $ sudo uefi-fw-tools.fwupdmgr refresh

6. Download firmware

 $ sudo uefi-fw-tools.fwupdmgr update

7. Reboot and ensure it start the upgrading process

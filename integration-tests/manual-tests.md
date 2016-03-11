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

8. Print the free space of the file system, replacing *{dev}* with the device that has the
   *writable* partition:

        sudo parted -s {dev} unit % print free

    * Check that the writable partition was resized to occupy all the empty space.

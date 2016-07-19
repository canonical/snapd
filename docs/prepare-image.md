# Bootstraping a snappy device

The `snap prepare-image` command is designed to do the snap specific
part of building an image.

## Running it

The `snap prepare-image` command takes a model assertion and a rootdir
as intput. It will create $ROOT/image that contains the directory layout
with content for the image. It will also create $ROOT/gadget that
contains the unpacked gadget snap content for ubuntu-image.

It will also inspect the gadget snap for the bootloader
configuration file and instlal that into the root-dir
into the right place.

### Example

```
$ cat > model.assertion <<EOF
type: model
series: 16
authority-id: my-brand
brand-id: my-brand
model: my-model
class: my-class
allowed-modes:  
required-snaps:  
architecture: armhf
store: canonical
gadget: canonical-pi2
kernel: canonical-pi2-linux
core: ubuntu-core
timestamp: 2016-01-02T10:00:00-05:00
body-length: 0

openpgpg 2cln
EOF

$ sudo snap prepare-image \
   --channel edge \
   model.assertion  \
   /tmp/prepare-image/
...
$ ls /tmp/prepare-image/gadget
boot-assets  canonical-pi2_6.snap  canonical-pi2_6.snap.sideinfo  meta

$ ls /tmp/prepare-image/image
boot  snap  var
```

# Future 

A way to load additional assertions into the image.


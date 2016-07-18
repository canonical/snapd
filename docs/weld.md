# Bootstraping a snappy device

The `snap weld` command is designed to do the snap specific part
of building an image.

## Running it

The `snap weld` command takes a model assertion as intput and
also `--root-dir` and `--gadget-unpack-dir`. It will place
all snaps in the root-dir and downloads/unpacks the gadget
snap into the directory specified with `--gadget-unpack-dir`.

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

$ sudo snap weld \
   --channel edge \
   --root-dir=/tmp/imageroot \
   --gadget-unpack-dir=/tmp/gadget-unpack-dir \
   model.assertion 
...
$ ls /tmp/gadget-unpack-dir/
boot-assets  canonical-pi2_6.snap  canonical-pi2_6.snap.sideinfo  meta

$ ls /tmp/imageroot
boot  snap  var
```

# Future 

A way to load additional assertions into the image.


# Bootstraping a snappy device

The `snap bootstrap` command is designed to do the snap specific part
of building an image.

## Phase 1

If the bootstrap command sees a `gadget-unpack-dir` key in the
yaml it will download the gadget snap found in the model assertion
into this directory. With that data tools like `ubuntu-image` can
create the partition layout and install the bootloader.

### Example

```
$ cat > bootstrap.yaml <<EOF
bootstrap:
 gadget-unpack-dir: /tmp/gadget-unpack-dir
 channel: edge
 model-assertion: model.assertion
EOF

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
timestamp: 2016-1-2T10:00:00-05:00
body-length: 0

openpgpg 2cln
EOF

$ sudo snap bootstrap bootstrap.yaml
...
$ ls /tmp/gadget-unpack-dir/
boot-assets  canonical-pi2_6.snap  canonical-pi2_6.snap.sideinfo  meta
```

## Phase 2

If the bootstrap command sees the `rootdir` key in the yaml it will
expect the image root directory with a basic bootloader setup. It
will download the requested snaps and put them into the right
place. It will also do the boot variable setup and extract kernel
assets (if needed).

### Example

```
$ cat > bootstrap.yaml <<EOF
bootstrap:
 rootdir: /tmp/diskimage-with-bootloader
 channel: edge
 model-assertion: model.assertion
 extra-snaps:
 - webdm
EOF

$ cat > model.assertion <<EOF
type: model
series: 16
authority-id: my-brand
brand-id: my-brand
model: my-model
class: my-class
allowed-modes:  
required-snaps: 
architecture: amd64
store: canonical
gadget: canonical-pc
kernel: canonical-pc-linux
core: ubuntu-core
timestamp: 2016-1-2T10:00:00-05:00
body-length: 0

openpgpg 2cln
EOF

$ sudo snap bootstrap bootstramp.yaml
[do the right thing]
```

# Supported fields in bootstrap.yaml

* model-assertions: the filename of the model assertion
* rootdir: the root directory of the image (e.g. `/tmp/diskimage`)
* gadget-unpack-dir: the directory that the gadget will get downloaded and unpacked to (e.g. `/tmp/gadget-unpack-dir`)
* channel: the channel to use (e.g. "edge")
* extra-snaps: list of string of snap names or paths (e.g. ["webdm", "hello"])


# Future fields we need to support

A way to load additional assertions into the image, e.g. via:

* assertions: a list of string of additional files with assertions

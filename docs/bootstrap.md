# Bootstraping a snappy device

The `snap bootstrap` command is designed to do the snap specific part
of building an image.

The bootstrap command will expect the image root directory with an
basic bootloader setup. It will download the requested snaps and
put them into the right place. It will also do the boot variable
setup and extract kernel assets (if needed).

## Example

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
os: ubuntu-core
timestamp: 2016-1-2T10:00:00-05:00
body-length: 0

openpgpg 2cln
EOF

$ sudo snap bootstrap bootstramp.yaml
[do the right thing]
```

## Supported fields in bootstrap.yaml

* model-assertions: the filename of the model assertion
* rootdir: the root directory of the image (e.g. "/tmp/diskimage")
* channel: the channel to use (e.g. "edge")
* extra-snaps: list of string of snap names or paths (e.g. ["webdm", "hello"])


## Future fields we need to support

A way to load additional assertions into the image, e.g. via:

* assertions: a list of string of additional files with assertions

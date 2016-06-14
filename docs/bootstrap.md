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
 snaps: 
 - canonical-pc
 - /tmp/os/b8X2psL1ryVrPt5WEmpYiqfr5emixTd7_122.snap
 - /tmp/kernel/SkKeDk2PRgBrX89DdgULk3pyY5DJo6Jk_30.snape
 EOF
 $ sudo snap bootstrap bootstramp.yaml
 ...
 ```

## Supported fields in bootstrap.yaml

* rootdir: the root directory of the image (e.g. "/tmp/diskimage")
* snaps: list of string of snap names or paths (e.g. ["canonical-pc", "hello"])
* channel: the channel to use (e.g. "edge")
* store-id: the store-id to use (e.g. "plano")
* architecture: the architecture to use (e.g. "armhf")





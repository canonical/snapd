# `snap-2-1-hdiffz`: a block plan over the source snap's compressed blocks

This package implements the third snap delta format. Where `snap-1-1-xdelta3`
diffs the *uncompressed* image and has the device rebuild the target by running
`mksquashfs` over a pseudo-file listing, a block plan describes the target as
instructions over the source image's own compressed blocks. Unchanged blocks are
copied still compressed, and only the blocks that actually changed are
compressed again.

That is the whole of the idea, and where the cost saving comes from. Rebuilding
with `mksquashfs` recompresses the entire image however little of it changed, and
re-derives every decision `mksquashfs` made -- duplicate detection, block
ordering, compressed-versus-raw -- which is why that format has to be handed the
build flags back. A plan re-derives nothing. It reproduces the target byte for
byte or refuses to exist.

## What a delta contains

    [header 128 B][section table n x 16 B][section payloads, in table order]

| section | what it holds |
|---|---|
| `SEC_SB` | the target's 96-byte superblock, verbatim |
| `SEC_CANARY` | a compressor self-check: bytes this machine produced, for the device to reproduce before anything else |
| `SEC_MDFRAME` | the target's metadata framing, one (uncompressed size, on-disk size) pair per block |
| `SEC_MDTAIL` | `[export_table_start, bytes_used)`, carried verbatim |
| `SEC_MDPATCH` | a patch from the source's metadata blob to the target's |
| `SEC_INSTR` | the instruction stream |
| `SEC_PAY` | patch blobs and literal bytes, in instruction order -- the only streamed section, and always last |
| `SEC_TOOLVER` | the tool versions the delta was built with, advisory only |

Metadata comes before the instructions so that it -- a few hundred KiB on a
large snap, and the highest-signal part of the delta -- is validated before a
single data byte is touched. There are three instructions:

- **copy** -- take *n* bytes of already-compressed data straight from the source
  image at some offset. No compressor runs, on either side.
- **literal** -- take the target's final on-disk bytes out of `SEC_PAY`. This is
  where a run lands that is not worth patching, and the fallback when no patch
  tool is installed.
- **patch run** -- reconstruct a run of blocks' plaintext by `hpatchz`-ing a
  window of source plaintext, then compress the result. This is the only
  instruction that asks the device for compression, and the only one the tuning
  below is about.

The header records the format version, the tools version and which patch tool
made the runs; the target's whole-file SHA-256 is what an apply is checked
against, so a delta reproduces padding too.

## Applying one

The applier reads the delta strictly forward, the source at random offsets, and
the target append-only. Its memory is bounded by `MaxRunUSize` (8 MiB by
default) rather than by the largest file in the image: a run's plaintext and its
source window are the only large things held at once, and both are memfds --
RAM, but not resident, and so reported separately from the resident set.

Applying needs `hpatchz` and the image's compressor. For xz that is the `xz`
binary, and it must be the same `xz` the image was built with; for lzo and zstd
it is snapd's own bundled `liblzo2`/`libzstd`, loaded with `dlopen` (see
[dynlib.go](dynlib.go)) and available only in a cgo build.

`ApplyToFile` assembles the image beside the target and renames it only once the
apply has succeeded, so the target path is either absent or the whole image.
That matters for the refusals made before any work is done -- a run cap the
caller cannot afford, a source the delta was not built against -- because in
exactly those cases the caller still has a usable snap where the target goes.

## Driving it from the command line

`snap delta` exposes the options above, which is how the measurements below were
taken and how a device with a ceiling is catered for. `snap delta --help` has
the full list; the two that matter outside a sweep are on the apply side:

    snap delta --apply --jobs 2 --max-run 4194304 --stats \
        -s old.snap -d snap.delta -t new.snap

`--jobs` bounds what the compressor holds, since each job carries its own
encoder state and block buffers, and `--max-run` bounds the scratch one patch
run holds. Together they are an apply's memory demand, and `--stats` reports
what it actually came to, peak resident size and peak scratch separately.

Generation takes the cost-model knobs -- `--window-ratio`, `--min-saving-rate`,
`--min-saving`, `--window-back` -- plus `--no-patch-runs` and `--no-path-match`
for the two baselines, `--run-log` for one line per run considered, and
`--hdiffz-args` for the diff tool itself. A knob left unset keeps the measured
default, so naming one does not disturb the rest. Options only this format reads
are refused rather than ignored when passed with another format, so a sweep
cannot quietly measure nothing.

## What it refuses

The format reproduces an image exactly or declines to describe it, at generate
time, before a delta exists. The store then offers the next format down and the
device falls back to a full download -- there is no failure mode where a delta
is offered and cannot be applied. Generation refuses an image that:

- uses a compressor this build does not reproduce (gzip, lzma, lz4, or lzo/zstd
  in a build without cgo),
- carries `COMPRESSOR_OPTIONS`, whose filter chain is not reproduced,
- uses fragments, which the extent walk does not describe,
- has no export table, so its inodes cannot be enumerated without walking
  directories,
- has an xattr table below the export table, so it is not carried verbatim,
- has a size that is not `bytes_used` rounded up to the 4 KiB padding -- which
  includes a snap that `Build` grew to `MinimumSnapSize`,
- or fails the generator's own verification: every delta is applied and compared
  against the real target before it is written out.

`SEC_CANARY` is the same check on the other side. It carries bytes this machine
produced so the device can reproduce them before doing any work, which catches a
compressor library whose output has drifted -- see the version note in
[zstd.go](zstd.go) for a case where it does.

## Measurements

Both formats through `snap delta` in this tree, on an amd64 workstation
(`mksquashfs` 4.7.5, `xz` 5.8.1, `hdiffz`/`hpatchz` 4.12.0). CPU is user+system
for the whole process tree; RSS is its peak resident.

| pair | format | delta | apply CPU | apply RSS |
|---|---|---|---|---|
| snapcraft 8.13.2 post75 -> post77 (73.7 MB) | `snap-1-1-xdelta3` | 1,191,821 (1.62%) | 48.3 s | 309 MiB |
| | `snap-2-1-hdiffz` | 544,533 (0.74%) | 17.2 s | 47 MiB |
| snapcraft 8.14.4 post129 -> post194 (73.4 MB) | `snap-1-1-xdelta3` | 4,875,219 (6.65%) | 53.8 s | 302 MiB |
| | `snap-2-1-hdiffz` | 2,095,733 (2.86%) | 27.3 s | 48 MiB |
| imx-kernel 1013.13 -> 1014.14 (53.5 MB) | `snap-1-1-xdelta3` | 6,604,634 (12.35%) | 32.6 s | 124 MiB |
| | `snap-2-1-hdiffz` | 3,486,257 (6.52%) | 23.0 s | 41 MiB |

Both produce a byte-identical target on every pair. The delta is smaller because
a plan spends its bytes on the blocks that changed instead of describing a whole
image; the apply is cheaper because most of the target needs no compressor at
all. How much of it is the pair's business: on post75 -> post77 the device
compresses 36.30 MiB of the target's 227.13 MiB of plaintext, 84.0% avoided,
while on the kernel pair -- one large, thoroughly reorganised file -- it
compresses 85.40 MiB of 101.56 MiB and only 15.9% is avoided. The kernel apply
is still the cheaper of the two formats, because even in its worst case a plan
compresses what changed rather than the whole image on top of `mksquashfs`'s own
bookkeeping.

Generating is the side that got more expensive, and deliberately so: it happens
once where the snap is published, on a machine with cores and memory, while the
apply happens on every device.

### Where the defaults come from

The numbers below come from the sweeps in the prototype this package was ported
from, whose deltas the port reproduces byte for byte. They are recorded here
because each is the argument for a constant, and a constant without its
measurement is just a number someone liked. The tuning itself is
`defaultPatchRunTuning` in [patchrun.go](patchrun.go), where each field's comment
carries the same reasoning next to the value it sets.

**`MaxRunUSize` = 8 MiB** is the applier's memory bound, since a run's plaintext
plus its 1.5x source window plus the patch are held at once. Raising it buys
nothing on either count. On the kernel pair, going 8 -> 16 -> 32 -> 64 MiB takes
the applier's scratch high-water mark from 20.75 to 41.08, 74.49 and 123.77 MiB
while the delta gets *larger* (21,026,657 -> 21,625,425 -> 23,167,660 ->
23,171,912 bytes) and the run count only falls from 26 to 19; on
post77 -> post129 the delta moves 0.2% across the whole range for scratch of
20.20 -> 35.16 MiB. Measured total demand at the default is roughly 33 MiB worst
case -- bounded by this constant, not by the largest file in the image. A caller
needing a tighter ceiling lowers it through `GenerateOpts.MaxRunUSize` and the
applier's negotiation enforces it, at the cost of a larger delta.

**`WindowRatio` = 1.5** has a knee. On post129 -> post194, ratio 0.5 gives a
16.56 MiB delta for 27.92 MiB decompressed, 1.0 gives 5.74/63.38, 1.5 gives
5.13/98.96, 2.0 gives 4.97/124.81, and 3.0 is worse on *both* counts at
5.00/144.17 -- past the knee a wider window only gives `hdiffz` more places to
find a mediocre match. 1.5 is within 3% of the best delta the ratio can reach
while decompressing a fifth less than 2.0 does.

**`WindowBackFrac` = 0** is a negative result, kept because it was cheap to
record. The hypothesis was that a window reaching only forward misses content
that moved later within a large file. On five pairs, 0, 0.1 and 0.25 land within
0.1% of each other (kernel: 3,486,147 / 3,483,920 / 3,483,009 bytes; post75 ->
post77: 544,423 / 545,551 / 546,517), and 0.4 and 0.5 are then ruinous -- the
kernel delta goes to 7,238,187 and 13,950,905 bytes, and post61 -> post60 from
100,705 to 1,390,666 and 2,743,381. The directions are not symmetric: budget
spent behind the anchor is budget not spent ahead of it, and a file's plaintext
runs forward from the offset the anchor names.

**`MinSaving` = 0**, i.e. no size floor on a patch run, is the measured surprise.
The floor was 16 KiB on the argument that a small run's forks cost more than
compressing its few blocks. Timed rather than assumed, a gadget revision whose
entire change is one 10.6 KiB run applies in 0.04 s of wall clock and no
measurable CPU either way, while the floor costs it a factor of ten in delta size
(1,083 bytes against 11,571). The large pairs agree: dropping the floor to 0
takes post75 -> post77 from 1,729,885 bytes to 544,423, post61 -> post60 from
1,147,575 to 100,705 and post77 -> post129 from 5,382,710 to 3,657,582, for 1.9,
1.6 and 3.3 more seconds of apply CPU -- and most of that is the 6-11% more
plaintext the extra runs compress, not the forks.

**`MinSavingRate` = 0.02** is the floor that replaces it, and unlike a size floor
it scales with the work asked: a run must save at least 0.02 delta bytes for
every byte of plaintext it makes the device compress, so a run of a single
128 KiB block has to save 2.6 KiB to pass. Read the rate as an exchange -- bytes of device compression
avoided per byte the delta grows -- and on post77 -> post129, the pair with the
most churn, that exchange decays steeply:

| rate | delta | device compresses | exchange vs. the default |
|---|---|---|---|
| 0.02 | 7,587,844 | 85.35 MiB | -- |
| 0.05 | 7,604,265 | 85.00 MiB | 22 B avoided per delta byte |
| 0.10 | 7,870,356 | 81.99 MiB | 12 |
| 0.20 | 10,753,615 | 62.79 MiB | 7.5 |
| 0.40 | 29,179,327 | 7.52 MiB | 3.8 |

By 0.20 it is refusing runs worth having. On quieter pairs the low rates are
inert -- the kernel pair produces an identical delta at 0.02, 0.05 and 0.10, and
post75 -> post77 at 0.02 and 0.05. So 0.02 is not the rate that minimises delta
size (that is 0) nor device CPU (raise it until runs stop); it is the largest
rate that was still purely favourable on every pair measured.

**Anchoring by path** ([match.go](match.go)) is what lets a run find its
counterpart when the image moved: target files are matched to source files by
name, rather than assuming a file's plaintext sits near the same offset. It is
free where nothing moved and decisive where something did. Three of six pairs
give an identical delta either way; the others come out 7.9%, 5.2% and 38.5%
smaller with it, the last being the kernel pair at 34,235,112 bytes anchored by
offset against 21,039,205 by path -- there, 14 of 23 anchors are found only by
recognising that a path's version component changed.
`GenerateOpts.NoPathMatch` turns it off, which is how the comparison was made.

## Interoperability

Deltas are told apart by their first four bytes, so this format's magic (`sqbp`)
is what dispatches an apply -- see `ApplyDelta` in [../delta.go](../delta.go).
The store negotiates by name: `snap-2-1-hdiffz` comes first in
`SupportedDeltaFormats`, and where generation refuses, the next format down is
offered instead.

`hdiffz` is needed only where deltas are made. Devices need `hpatchz`, which is
why the snapd snap ships that one and not both.

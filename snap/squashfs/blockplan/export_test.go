// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package blockplan

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sort"
)

// This file exports what the tests need to reach inside the package. The tests
// themselves live in package blockplan_test and drive the code the way the rest
// of snapd does, but most of this package is unexported on purpose -- the format
// is one API, not thirty -- and the parts that carry the risk are the internal
// ones: the block walks, the instruction encoding, the run costing.

// --- compressor.go ---

type BlobDecoder = blobDecoder

var (
	NewCompressor          = newCompressor
	CompressorImplemented  = compressorImplemented
	ImplementedCompressors = implementedCompressors
	CompressorName         = compressorName
	DecompressBlob         = decompressBlob
	CheckCompressorMatches = checkCompressorMatches
	ResolveJobs            = resolveJobs
	RunParallel            = runParallel
	NewPlainFile           = newPlainFile
)

type PlainBytes = plainBytes

// ImplementedCompressorIDs lists the ids this build registered, in a stable
// order so that a failure names the same compressor run to run. It is how the
// contract tests cover every codec a build has rather than a fixed list.
func ImplementedCompressorIDs() []uint16 {
	ids := make([]uint16, 0, len(compressorFactories))
	for id := range compressorFactories {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// MockCompressor registers a compressor and the section codec that goes with it
// for the duration of a test, so that the dispatch and refusal paths can be
// driven without depending on which codecs this build happens to include.
func MockCompressor(id uint16, mk func(jobs int) (Compressor, error), codec uint16, dec BlobDecoder) (restore func()) {
	oldMk, hadMk := compressorFactories[id]
	oldDec, hadDec := blobDecoders[codec]
	registerCompressor(id, mk, codec, dec)
	return func() {
		if hadMk {
			compressorFactories[id] = oldMk
		} else {
			delete(compressorFactories, id)
		}
		if hadDec {
			blobDecoders[codec] = oldDec
		} else {
			delete(blobDecoders, codec)
		}
	}
}

// --- tool.go ---

var (
	ToolCommand = toolCommand
	HaveTool    = haveTool
)

func MockCommandFromSystemSnapWithContext(f func(ctx context.Context, name string, args ...string) (*exec.Cmd, error)) (restore func()) {
	old := snapdtoolCommandFromSystemSnapWithContext
	snapdtoolCommandFromSystemSnapWithContext = f
	return func() {
		snapdtoolCommandFromSystemSnapWithContext = old
	}
}

// --- xz.go ---

type XZBlockSplitter = xzBlockSplitter

var (
	NewXZBlockSplitter = newXZBlockSplitter
	AppendXZFrame      = appendXZFrame
	XZDecompressAll    = xzDecompressAll
	XZBlockHeaderSize  = blockHeaderSize
)

// Next walks to the following block, as CompressBlocks does over a running xz.
func (s *xzBlockSplitter) Next() (payload []byte, uSize int, err error) {
	return s.next()
}

// --- image.go ---
//
// The image-side types are unexported, but their fields keep the names the
// on-disk structures go by, so an alias is enough for a test to read them.

type (
	Image     = squashfsImage
	FileInode = fileInode
	MetaBlock = metaBlock
)

const (
	SquashfsMagic         = squashfsMagic
	SquashfsMetadataSize  = squashfsMetadataSize
	SuperblockSize        = superblockSize
	NoTable               = squashfsNoTable
	InodeTypeFile         = inodeTypeFile
	InodeTypeExtFile      = inodeTypeExtFile
	FlagCompressorOptions = flagCompressorOptions
	CompressorXz          = compressorXz
)

var (
	OpenImage       = openImage
	ParseSuperblock = parseSuperblock
	PaddedImageSize = paddedImageSize
)

// CheckSupportedGeometry vets a superblock on its own, as the applier does with
// the target's before the image it describes exists.
func (sb *superblock) CheckSupportedGeometry(imageSize int64) error {
	return sb.checkSupportedGeometry(imageSize)
}

func (im *squashfsImage) CheckSupported() error { return im.checkSupported() }

func (im *squashfsImage) InodeExtents(fi *fileInode) ([]fileBlock, error) {
	return im.inodeExtents(fi)
}

// --- dir.go ---

type (
	FileEntry  = fileEntry
	MetaRegion = metaRegion
	DirListing = dirListing
	DirChild   = dirChild
)

// NewMetaRegion assembles a region holding a single metadata block, so the
// listing tests can provoke framing errors that no real image can be made to
// produce: a real directory table lives inside compressed metadata blocks, and
// rewriting one of those means recompressing it.
func NewMetaRegion(blob []byte) *metaRegion {
	return &metaRegion{
		Start:  0,
		Blob:   blob,
		Blocks: []metaBlock{{Offset: 0, CSize: len(blob), USize: len(blob)}},
		index:  map[uint64]int{0: 0},
	}
}

func (im *squashfsImage) ReadDirListing(meta *metaRegion, dirRel int64, list dirListing) ([]dirChild, error) {
	return im.readDirListing(meta, dirRel, list)
}

// --- plan.go ---
//
// The container's own structures keep the field names the format's byte layout
// goes by, so the tests read them through aliases; only the methods, which are
// the package's internal API, need wrappers.

type (
	PlanHeader   = planHeader
	PlanWriter   = planWriter
	SectionEntry = sectionEntry
)

const (
	PlanHeaderSize    = planHeaderSize
	SectionEntrySize  = sectionEntrySize
	PlanFormatVersion = planFormatVersion
	PlanToolsVersion  = planToolsVersion
	PatchToolHdiffz   = patchToolHdiffz

	SecSB      = secSB
	SecMDFrame = secMDFrame
	SecMDTail  = secMDTail
	SecMDPatch = secMDPatch
	SecInstr   = secInstr
	SecPay     = secPay
)

var (
	ParsePlanHeader = parsePlanHeader
	OpenPlan        = openPlan
)

func (h *planHeader) Marshal() []byte  { return h.marshal() }
func (e sectionEntry) Marshal() []byte { return e.marshal() }

func (w *planWriter) AddSection(id uint16, raw []byte) { w.addSection(id, raw) }

func (w *planWriter) AddSectionCompressed(ctx context.Context, id uint16, raw []byte, comp Compressor) error {
	return w.addSectionCompressed(ctx, id, raw, comp)
}

func (w *planWriter) AddSectionFile(id uint16, f *os.File, storedLen int, crc uint32) {
	w.addSectionFile(id, f, storedLen, crc)
}

// Emit is writeTo under another name: a WriteTo(io.Writer) error method would
// be mistaken for io.WriterTo, whose signature it is not.
func (w *planWriter) Emit(out io.Writer) error { return w.writeTo(out) }

func (pr *planReader) Section(id uint16) []byte  { return pr.section(id) }
func (pr *planReader) HasSection(id uint16) bool { return pr.hasSection(id) }

// Entries, Pay and PayLen are the reader's state a test has to see: which codec
// a section ended up stored under, and that the payload really was left as a
// stream positioned at its first byte rather than read into memory.
func (pr *planReader) Entries() []sectionEntry { return pr.entries }
func (pr *planReader) Pay() io.Reader          { return pr.pay }
func (pr *planReader) PayLen() int64           { return pr.payLen }

// --- instr.go ---
//
// The instruction stream's own structures, again read through aliases: the
// encoder and the decoder are what the tests drive, and both are the format's
// business rather than the package's API.

type (
	Opcode       = opcode
	Instruction  = instruction
	PlanBlock    = planBlock
	SrcWindow    = srcWindow
	InstrEncoder = instrEncoder
	InstrDecoder = instrDecoder
)

const (
	OpCopy     = opCopy
	OpPatchRun = opPatchRun
	OpLiteral  = opLiteral
)

var (
	NewInstrEncoder = newInstrEncoder
	NewInstrDecoder = newInstrDecoder
	EncodeMDFrame   = encodeMDFrame
	DecodeMDFrame   = decodeMDFrame
)

func (d *instrDecoder) Done() bool { return d.done() }

// Rest is how many bytes of the stream are still unread, which is the one thing
// a decoding loop cannot tell from the instructions it got back.
func (d *instrDecoder) Rest() int { return len(d.buf) - d.pos }

// --- match.go ---
//
// The matcher is the one part of the generator whose mistakes are invisible: a
// missed correspondence is not an error, it is just a bigger delta. So its
// decision table is driven directly rather than only through a generate.

type (
	FileLayout  = fileLayout
	FileBlock   = fileBlock
	PathMatcher = pathMatcher
	AnchorKind  = anchorKind
)

const (
	AnchorNone  = anchorNone
	AnchorPath  = anchorPath
	AnchorFuzzy = anchorFuzzy
)

var (
	NormalizeDigits = normalizeDigits
	PathsMatchFuzzy = pathsMatchFuzzy
	SizeSimilarity  = sizeSimilarity
)

// NewFileBlock builds one file block. The literal cannot be written outside the
// package: a block's on-disk fields are promoted from an embedded unexported
// type, so they can be read from a test but not set by one.
func NewFileBlock(imageOff int64, cSize, uSize int, uOff int64) fileBlock {
	return fileBlock{extent: extent{Offset: imageOff, CSize: cSize, USize: uSize}, UOff: uOff}
}

// NewPathMatcherFor assembles a matcher from file layouts rather than from a pair
// of images, so that which index answers -- and when neither does -- can be
// asserted on shapes no fixture pair reliably produces. The layouts go in
// through the same two methods a real generate indexes with, so a test cannot
// end up matching against an index production code would never have built.
func NewPathMatcherFor(src, tgt []*fileLayout) *pathMatcher {
	m := newEmptyPathMatcher()
	for _, f := range src {
		m.addSource(f)
	}
	for _, f := range tgt {
		m.addTarget(f)
	}
	return m
}

func (f *fileLayout) UOffAt(uOff int64) (int64, bool) { return f.uOffAt(uOff) }

func (m *pathMatcher) Anchor(tgtOff int64) (int64, anchorKind) { return m.anchor(tgtOff) }

// --- patchtool.go ---
//
// The three ways a patch tool is driven. Nothing about them is exported for real
// callers, because a patch run is an implementation detail of generate and apply
// -- but they are the one place where correctness rests on an external tool
// behaving as it was measured to.

var (
	RunHdiffz       = runHdiffz
	HdiffzArgs      = hdiffzArgs
	RunHpatchz      = runHpatchz
	RunHpatchzFiles = runHpatchzFiles
)

// --- toolver.go ---

var (
	ToolVersionLine     = toolVersionLine
	CaptureToolVersions = captureToolVersions
	CheckToolVersions   = checkToolVersions
)

// --- memfd.go ---

type MemFile = memFile

var (
	NewMemFile     = newMemFile
	NewMemFileWith = newMemFileWith
	NewDiskFile    = newDiskFile
)

// OnDisk reports which backing a scratch file got, which is the one thing about
// it a caller never needs to know and a test must.
func (m *memFile) OnDisk() bool { return m.onDisk }

package snappy

import (
	"fmt"

	"github.com/cheggaaa/pb"
)

type ProgressMeter interface {
	Start(total int64)
	Increment()
	Finished()

	// interface for writer
	Write(p []byte) (n int, err error)
}

type TextProgress struct {
	ProgressMeter
	pbar *pb.ProgressBar
	pkg  string
}

func NewTextProgress(pkg string) *TextProgress {
	t := TextProgress{pbar: pb.New64(0)}
	t.pbar.ShowSpeed = true
	return &t
}

func (t *TextProgress) Start(total int64) {
	fmt.Println("Starting download of", t.pkg)
	t.pbar.Total = total
	t.pbar.Start()
}

func (t *TextProgress) Increment() {
	t.pbar.Increment()
}

func (t *TextProgress) Finished() {
	t.pbar.FinishPrint("Done")
}

func (t *TextProgress) Write(p []byte) (n int, err error) {
	return t.pbar.Write(p)
}

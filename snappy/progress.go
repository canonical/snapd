package snappy

import (
	"fmt"

	"github.com/cheggaaa/pb"
)

type ProgressMeter interface {
	Start(msg string, total int64)
	Increment()
	Finished(msg string)

	// interface for writer
	Write(p []byte) (n int, err error)
}

type TextProgress struct {
	pbar *pb.ProgressBar
}

func NewTextProgress() *TextProgress {
	t := TextProgress{pbar: pb.New64(0)}
	t.pbar.ShowSpeed = true
	return &t
}
func (t *TextProgress) Start(msg string, total int64) {
	fmt.Println(msg)
	t.pbar.Total = total
	t.pbar.Start()
}
func (t *TextProgress) Increment() {
	t.pbar.Increment()
}
func (t *TextProgress) Finished(msg string) {
	t.pbar.FinishPrint(msg)
}
func (t *TextProgress) Write(p []byte) (n int, err error) {
	return t.pbar.Write(p)
}

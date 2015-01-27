package snappy

import (
	"fmt"

	"github.com/cheggaaa/pb"
)

type ProgressMeter interface {
	Start(total float64)
	Set(current float64)
	Finished()

	// FIXME: better name?
	// Show progress via a spinner
	Spin(msg string)

	// interface for writer
	Write(p []byte) (n int, err error)
}

type TextProgress struct {
	ProgressMeter
	pbar     *pb.ProgressBar
	pkg      string
	spinStep int
}

func NewTextProgress(pkg string) *TextProgress {
	t := TextProgress{pbar: pb.New64(0)}
	t.pbar.ShowSpeed = true
	return &t
}

func (t *TextProgress) Start(total float64) {
	fmt.Println("Starting download of", t.pkg)
	t.pbar.Total = int64(total)
	t.pbar.Start()
}

func (t *TextProgress) Set(current float64) {
	t.pbar.Set(int(current))
}

func (t *TextProgress) Finished() {
	t.pbar.FinishPrint("Done")
}

func (t *TextProgress) Write(p []byte) (n int, err error) {
	return t.pbar.Write(p)
}

func (t *TextProgress) Spin(msg string) {
	states := `|/-\`
	fmt.Printf("\r%s[%c]", msg, states[t.spinStep])
	t.spinStep++
	if t.spinStep >= len(states) {
		t.spinStep = 0
	}
}

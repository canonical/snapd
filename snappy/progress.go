package snappy

import (
	"bufio"
	"fmt"
	"os"
	"unicode"

	"github.com/cheggaaa/pb"
)

// ProgressMeter is an interface to show progress to the user
type ProgressMeter interface {
	// Start progress with max "total" steps
	Start(total float64)

	// set progress to the "current" step
	Set(current float64)

	// set "total" steps needed
	SetTotal(total float64)

	// Finish the progress display
	Finished()

	// Indicate indefinite activity by showing a spinner
	Spin(msg string)

	// interface for writer
	Write(p []byte) (n int, err error)

	// ask the user whether they agree to the given license's text
	Agreed(intro, licenseFile string) bool
}

// NullProgress is a ProgressMeter that does nothing
type NullProgress struct {
}

// Start does nothing
func (t *NullProgress) Start(total float64) {
}

// Set does nothing
func (t *NullProgress) Set(current float64) {
}

// SetTotal does nothing
func (t *NullProgress) SetTotal(total float64) {
}

// Finished does nothing
func (t *NullProgress) Finished() {
}

// Write does nothing
func (t *NullProgress) Write(p []byte) (n int, err error) {
	return n, nil
}

// Spin does nothing
func (t *NullProgress) Spin(msg string) {
}

// Agreed does nothing
func (t *NullProgress) Agreed(intro, licenseFile string) bool {
	return false
}

// TextProgress show progress on the terminal
type TextProgress struct {
	ProgressMeter
	pbar     *pb.ProgressBar
	pkg      string
	spinStep int
}

// NewTextProgress returns a new TextProgress type
func NewTextProgress(pkg string) *TextProgress {
	return &TextProgress{pkg: pkg}
}

// Start starts showing progress
func (t *TextProgress) Start(total float64) {
	fmt.Println("Starting download of", t.pkg)

	// TODO go to New64 once we update the pb package.
	t.pbar = pb.New(0)
	t.pbar.Total = int64(total)
	t.pbar.ShowSpeed = true
	t.pbar.Units = pb.U_BYTES
	t.pbar.Start()
}

// Set sets the progress to the current value
func (t *TextProgress) Set(current float64) {
	t.pbar.Set(int(current))
}

// SetTotal set the total steps needed
func (t *TextProgress) SetTotal(total float64) {
	t.pbar.Total = int64(total)
}

// Finished stops displaying the progress
func (t *TextProgress) Finished() {
	if t.pbar != nil {
		t.pbar.FinishPrint("Done")
	}
}

// Write is there so that progress can implment a Writer and can be
// used to display progress of io operations
func (t *TextProgress) Write(p []byte) (n int, err error) {
	return t.pbar.Write(p)
}

// Spin advances a spinner, i.e. can be used to show progress for operations
// that have a unknown duration
func (t *TextProgress) Spin(msg string) {
	states := `|/-\`
	fmt.Printf("\r%s[%c]", msg, states[t.spinStep])
	t.spinStep++
	if t.spinStep >= len(states) {
		t.spinStep = 0
	}
}

// Agreed asks the user whether they agree to the given license text
func (t *TextProgress) Agreed(intro, license string) bool {
	if _, err := fmt.Println(intro); err != nil {
		return false
	}

	// XXX: send it through a pager instead of this ugly thing
	if _, err := fmt.Println(license); err != nil {
		return false
	}

	reader := bufio.NewReader(os.Stdin)
	if _, err := fmt.Print("Do you agree? [y/n] "); err != nil {
		return false
	}
	r, _, err := reader.ReadRune()
	if err != nil {
		return false
	}

	return unicode.ToLower(r) == 'y'
}

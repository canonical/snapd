package snapdtool_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snapdtool"
)

const dataOK = `one line
another line
yadda yadda
VERSION=42
potatoes
`

const dataNOK = `a line
another
this is a very long line
that wasn't long what are you talking about long lines are like, so long you need to add things like commas to them for them to even make sense
a short one
and another
what is this
why
no
stop
`

const dataHuge = `Lorem ipsum dolor sit amet, consectetur adipiscing elit.
Quisque euismod ac elit ac auctor.
Proin malesuada diam ac tellus maximus aliquam.
Aenean tincidunt mi et tortor bibendum fringilla.
Phasellus finibus, urna id convallis vestibulum, metus metus venenatis massa, et efficitur nisi elit in massa.
Mauris at nisl leo.
Nulla ullamcorper risus venenatis massa venenatis, ac finibus lacus aliquam.
Nunc tempor convallis cursus.
Maecenas id rhoncus orci, eget pretium eros.

Donec et consectetur lacus.
Nam nec mattis elit, id sollicitudin magna.
Aenean sit amet diam vitae tellus finibus tristique.
Duis et pharetra tortor, id pharetra erat.
Suspendisse commodo venenatis blandit.
Morbi tellus est, iaculis et tincidunt nec, semper ut ipsum.
Mauris quis condimentum risus.
Lorem ipsum dolor sit amet, consectetur adipiscing elit.
Mauris gravida turpis ut urna laoreet, sit amet tempor odio porttitor.

Aliquam nibh libero, venenatis ac vehicula at, blandit id odio.
Etiam malesuada consectetur porta.
Fusce consectetur ligula et metus interdum sollicitudin.
Pellentesque odio neque, pharetra et gravida non, vestibulum nec lorem.
Sed condimentum velit ex, sit amet viverra lectus aliquet quis.
Aliquam tincidunt eu elit at condimentum.
Donec feugiat urna tortor, pellentesque tincidunt quam congue eu.

Phasellus vel libero molestie, semper erat at, suscipit nisi.
Nullam euismod neque ut turpis molestie, eu fringilla elit volutpat.
Phasellus maximus, urna eget porta congue, diam enim volutpat diam, nec ultrices lorem risus ac metus.
Vivamus convallis eros non nunc pretium bibendum.
Maecenas consectetur metus metus.
Morbi scelerisque urna at arcu tristique feugiat.
Vestibulum condimentum odio sed tortor vulputate, eget hendrerit mi consequat.
Integer egestas finibus augue, ac scelerisque ex pretium aliquam.
Aliquam erat volutpat.
Suspendisse a nulla ultrices, porttitor tellus ut, bibendum diam.
In nibh dui, tempus eget vestibulum in, euismod in ex.
In tempus felis lectus.

Maecenas suscipit turpis eget velit molestie, quis luctus nibh placerat.
Nulla semper eleifend nisi ut dignissim.
Donec eu massa maximus, blandit massa ac, lobortis risus.
Donec id condimentum libero, vel fringilla diam.
Praesent ultrices, ante congue sollicitudin sagittis, orci ex maximus ipsum, at convallis nunc nisl nec lorem.
Duis iaculis finibus fermentum.
Curabitur quis pharetra metus.
Donec nisl ipsum, faucibus vitae odio sed, mattis feugiat nisl.
Pellentesque nec justo in magna volutpat accumsan.
Pellentesque porttitor justo non velit porta rhoncus.
Nulla ut lectus quis lectus rutrum dignissim.
Pellentesque posuere sagittis felis, quis varius purus pharetra eu.
Nam blandit diam ullamcorper, auctor massa at, aliquet dui.
Aliquam erat volutpat.
Nullam sit amet augue nec diam sollicitudin ullamcorper a vitae neque.
VERSION=42
`

func benchmarkCSRE(b *testing.B, data string) {
	tempdir, err := os.MkdirTemp("", "")
	if err != nil {
		b.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(tempdir)
	if err = os.MkdirAll(filepath.Join(tempdir, dirs.CoreLibExecDir), 0755); err != nil {
		b.Fatalf("mkdirall: %v", err)
	}

	if err = os.WriteFile(filepath.Join(tempdir, dirs.CoreLibExecDir, "info"), []byte(data), 0600); err != nil {
		b.Fatalf("%v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapdtool.SystemSnapSupportsReExec(tempdir)
	}
}

func BenchmarkCSRE_fakeOK(b *testing.B)   { benchmarkCSRE(b, dataOK) }
func BenchmarkCSRE_fakeNOK(b *testing.B)  { benchmarkCSRE(b, dataNOK) }
func BenchmarkCSRE_fakeHuge(b *testing.B) { benchmarkCSRE(b, dataHuge) }

func BenchmarkCSRE_real(b *testing.B) {
	for i := 0; i < b.N; i++ {
		snapdtool.SystemSnapSupportsReExec("/snap/core/current")
	}
}

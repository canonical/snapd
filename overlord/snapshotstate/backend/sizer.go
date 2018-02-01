package backend

type sizer struct {
	size int64
}

func (sz *sizer) Write(data []byte) (n int, err error) {
	n = len(data)
	sz.size += int64(n)
	return
}

func (sz *sizer) Reset() {
	sz.size = 0
}

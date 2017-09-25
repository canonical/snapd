package progress

var FormatAmount = formatAmount
var FormatBPS = formatBPS
var FormatDuration = formatDuration
var ClrEOL = clrEOL
var ExitAttributeMode = exitAttributeMode

func MockEmptyEscapes() func() {
	oldClrEOL := clrEOL
	oldCursorInvisible := cursorInvisible
	oldCursorVisible := cursorVisible
	oldEnterReverseMode := enterReverseMode
	oldExitAttributeMode := exitAttributeMode

	clrEOL = ""
	cursorInvisible = ""
	cursorVisible = ""
	enterReverseMode = ""
	exitAttributeMode = ""

	return func() {
		clrEOL = oldClrEOL
		cursorInvisible = oldCursorInvisible
		cursorVisible = oldCursorVisible
		enterReverseMode = oldEnterReverseMode
		exitAttributeMode = oldExitAttributeMode
	}
}

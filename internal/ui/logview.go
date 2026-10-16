package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// logView shows the log as normal, selectable text. A disabled Entry was used
// before, and Fyne draws disabled text so faint it was unreadable.
type logView struct {
	label  *widget.Label
	scroll *container.Scroll
}

func newLogView() *logView {
	l := widget.NewLabel("")
	l.Wrapping = fyne.TextWrapBreak
	l.Selectable = true
	return &logView{label: l, scroll: container.NewVScroll(l)}
}

func (v *logView) Set(text string) {
	if v.label.Text == text {
		return
	}
	v.label.SetText(text)
	v.scroll.ScrollToBottom()
}

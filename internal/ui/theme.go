package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

//go:embed fonts/NotoSansSC-Regular.ttf
var notoTTF []byte

type fontResource struct{}

func (fontResource) Name() string { return "NotoSansSC-Regular.ttf" }
func (fontResource) Content() []byte {
	return notoTTF
}

type cjkTheme struct{ fyne.Theme }

func newCJKTheme() fyne.Theme {
	return cjkTheme{Theme: theme.DefaultTheme()}
}

func (t cjkTheme) Font(fyne.TextStyle) fyne.Resource {
	return fontResource{}
}

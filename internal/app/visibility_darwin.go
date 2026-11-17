package app

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

func followWindowVisibility(win *mygo.Window, changed func(bool)) {
	window := objc.ID(win.NativeHandle())
	visible := func() bool {
		return objc.Send[uint](window, objc.RegisterName("occlusionState"))&2 != 0
	}
	center := objc.ID(objc.GetClass("NSNotificationCenter")).Send(objc.RegisterName("defaultCenter"))
	name := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), "NSWindowDidChangeOcclusionStateNotification")
	block := objc.NewBlock(func(_ objc.Block, _ objc.ID) { changed(visible()) })
	observer := center.Send(objc.RegisterName("addObserverForName:object:queue:usingBlock:"), name, window, objc.ID(0), block)
	win.OnClosed(func() {
		center.Send(objc.RegisterName("removeObserver:"), observer)
		block.Release()
	})
	changed(visible())
}

package native

import (
	"sheaf/internal/native/shell"
	"sheaf/internal/native/track"
	"sheaf/internal/native/view"
)

// viewDef registers a view of the workspace window.
type viewDef struct {
	id   view.ID
	make func(view.Host) view.View
	dock func() shell.Dock // the arrangement of a first launch
	// dockVer drops layouts made with obsolete pane defaults.
	dockVer int
}

// registry lists the workspace views; the first opens with the window.
var registry = []viewDef{
	{id: view.Track, make: func(h view.Host) view.View { return track.New(h) }, dock: track.Dock, dockVer: 2},
}

func def(id view.ID) *viewDef {
	for i := range registry {
		if registry[i].id == id {
			return &registry[i]
		}
	}
	return nil
}

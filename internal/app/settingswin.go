package app

import "github.com/egoist/mygo"

func settingsOptions() mygo.WindowOptions {
	return mygo.WindowOptions{
		Width:     760,
		Height:    600,
		MinWidth:  640,
		MinHeight: 480,
		StateKey:  "settings",
	}
}

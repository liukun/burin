package windowrules

// nextWindowID returns the ID of the window to cycle to among the
// windows of ids, sorted ascending, when the one of mainID is main, or 0
// for none is: the first ID above mainID, wrapping around to the first
// after the last, so the first when none is main. A mainID not among ids
// still finds its place in the order. With fewer than two windows there
// is nothing to cycle to, and it returns 0.
func nextWindowID(mainID uint32, ids []uint32) uint32 {
	if len(ids) < 2 {
		return 0
	}
	for _, id := range ids {
		if id > mainID {
			return id
		}
	}
	return ids[0]
}

// windowToRaise returns the ID of the window to raise among the windows
// of ids, sorted ascending, when the one of mainID is main, or 0 for
// none is, and whether the app is frontmost. The lowest ID is the
// app's primary window, every other one a child window. A frontmost app
// moves on to nextWindowID. One in the background keeps a child window
// that is main, which a mainID above the lowest, listed or not, means,
// and returns 0 for it to be only brought forward; with the primary
// window main, or none, it returns the first child, the ID after the
// lowest. With fewer than two windows there is nothing to raise, and it
// returns 0.
func windowToRaise(mainID uint32, ids []uint32, appFrontmost bool) uint32 {
	if len(ids) < 2 {
		return 0
	}
	if appFrontmost {
		return nextWindowID(mainID, ids)
	}
	childIsMain := mainID > ids[0]
	if childIsMain {
		return 0
	}
	return ids[1]
}

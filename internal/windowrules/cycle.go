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

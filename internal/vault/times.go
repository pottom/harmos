package vault

import (
	"time"

	gokeepasslib "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// nowPtr is a fresh timestamp, freshly allocated.
//
// The allocation is the point. gokeepasslib's NewTimeData points CreationTime,
// LastModificationTime, LastAccessTime and LocationChanged at one shared
// variable, so writing through any of them writes all four. Assigning a new
// pointer each time is what keeps "modified" from silently rewriting "created".
func nowPtr() *w.TimeWrapper {
	return &w.TimeWrapper{Time: time.Now().UTC()}
}

// timePtr is nowPtr for a specific instant.
func timePtr(t time.Time) *w.TimeWrapper {
	return &w.TimeWrapper{Time: t.UTC()}
}

// freshTimes is NewTimeData with the aliasing undone: four independent pointers
// holding the same instant, so each can move on its own afterwards.
func freshTimes() gokeepasslib.TimeData {
	td := gokeepasslib.NewTimeData()
	now := time.Now().UTC()
	td.CreationTime = timePtr(now)
	td.LastModificationTime = timePtr(now)
	td.LastAccessTime = timePtr(now)
	td.LocationChanged = timePtr(now)
	return td
}

// cloneTimes copies a TimeData so that later edits to the original cannot reach
// through the shared pointers and rewrite the copy. Without this a history
// record would follow the live entry's timestamps instead of preserving its own.
func cloneTimes(td gokeepasslib.TimeData) gokeepasslib.TimeData {
	out := td
	clone := func(t *w.TimeWrapper) *w.TimeWrapper {
		if t == nil {
			return nil
		}
		c := *t
		return &c
	}
	out.CreationTime = clone(td.CreationTime)
	out.LastModificationTime = clone(td.LastModificationTime)
	out.LastAccessTime = clone(td.LastAccessTime)
	out.ExpiryTime = clone(td.ExpiryTime)
	out.LocationChanged = clone(td.LocationChanged)
	return out
}

// touch records that an entry changed now, without disturbing its creation time.
func touch(e *gokeepasslib.Entry) {
	e.Times.LastModificationTime = nowPtr()
	e.Times.LastAccessTime = nowPtr()
}

// touchGroup is touch for a group.
func touchGroup(g *gokeepasslib.Group) {
	g.Times.LastModificationTime = nowPtr()
	g.Times.LastAccessTime = nowPtr()
}

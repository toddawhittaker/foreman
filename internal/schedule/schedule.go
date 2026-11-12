// Package schedule restarts the active world at a set time. It is a leaf
// package that uses only the game controller's public methods.
package schedule

// Scheduler runs the restart schedule.
type Scheduler struct{}

// New returns a Scheduler. It restarts nothing yet.
func New() *Scheduler { return &Scheduler{} }

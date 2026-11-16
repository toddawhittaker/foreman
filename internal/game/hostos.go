package game

import "context"

// HostOS is the system Docker runs containers on, read from the daemon's
// version answer ("linux", "windows"). It is empty when the daemon does not
// say, and the error is Docker's when it cannot be asked.
func (c *Controller) HostOS(ctx context.Context) (string, error) {
	v, err := c.docker.Version(ctx)
	return v.Os, err
}

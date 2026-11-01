package jd

import "time"

// TimeoutFor exposes the per-path time limit to the tests.
func (c *Client) TimeoutFor(path string) time.Duration { return c.timeoutFor(path) }

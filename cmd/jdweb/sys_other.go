//go:build !linux

package main

import "log/slog"

// dropPrivileges does nothing outside Linux: the Docker image is Linux only.
func dropPrivileges(string, int, int, *slog.Logger) error { return nil }

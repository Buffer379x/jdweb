//go:build linux

package main

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// dropPrivileges: started as root (Unraid template, --user 0:0), JDWeb gives
// the data directory to PUID:PGID and drops root at once, before it opens the
// database or a port. Started as anybody else it does nothing.
func dropPrivileges(dataDir string, uid, gid int, log *slog.Logger) error {
	if os.Getuid() != 0 {
		return nil
	}
	if uid == 0 || gid == 0 {
		return fmt.Errorf("JDWeb is running as root, so PUID and PGID must name the user to run as, and not be 0")
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return writableHint(dataDir, uid, gid, err)
	}
	changed := 0
	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && (int(st.Uid) != uid || int(st.Gid) != gid) {
			changed++
			return os.Lchown(path, uid, gid)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cannot give %s to %d:%d: %w", dataDir, uid, gid, err)
	}
	if err := syscall.Setgroups([]int{gid}); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	log.Info("dropped root", "uid", uid, "gid", gid, "chowned", changed)
	return nil
}

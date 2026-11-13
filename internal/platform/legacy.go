package platform

import (
	"os"
	"path/filepath"
)

// The app used to be called Iddler. These names let an existing install carry over:
// accounts, settings and the login item move to the new names on the first launch.
const legacyName = "Iddler"

// moveDir renames the old data folder to the new one when only the old one exists.
func moveDir(oldDir, newDir string) {
	if _, err := os.Stat(newDir); err == nil {
		return
	}
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(newDir), 0o700)
	os.Rename(oldDir, newDir)
}

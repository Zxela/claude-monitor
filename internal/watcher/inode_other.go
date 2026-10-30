//go:build !unix

package watcher

import "os"

func inodeOf(os.FileInfo) uint64 { return 0 }

//go:build !windows

package legacycold

import (
	"os"

	"qimeng.local/next/server/internal/storage"
)

func unsafeFileInfo(info os.FileInfo) bool { return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 }
func unsafeEntry(entry os.DirEntry) bool   { return entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 }

func publishSidecarOrArchive(out *readRoot, name string) error {
	return storage.PublishTemp(out.root, ".incomplete/"+name, name)
}

//go:build !windows

package legacycold

import "qimeng.local/next/server/internal/storage"

func publishSidecarOrArchive(out *readRoot, name string) error {
	return storage.PublishTemp(out.root, ".incomplete/"+name, name)
}

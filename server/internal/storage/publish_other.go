//go:build !linux

package storage

import "os"

func PublishTemp(root *os.Root, from, to string) error {
	return linkAndRemoveStage(root, from, to)
}

package storage

import (
	"errors"
	"os"
	"path"
)

// PublishTemp consumes an owned staging file after an atomic, no-replace
// publication on the same storage. The caller manages directory durability.
func (r *Root) PublishTemp(from, to string) error {
	source, old, err := r.resolve(from)
	if err != nil {
		return err
	}
	target, next, err := r.resolve(to)
	if err != nil {
		return err
	}
	if source.root != target.root {
		return errors.New("staging and destination must share a storage root")
	}
	if err = target.root.MkdirAll(path.Dir(next), 0700); err != nil {
		return err
	}
	return PublishTemp(source.root, old, next)
}

func linkAndRemoveStage(root *os.Root, from, to string) error {
	if err := root.Link(from, to); err != nil {
		return err
	}
	return root.Remove(from)
}

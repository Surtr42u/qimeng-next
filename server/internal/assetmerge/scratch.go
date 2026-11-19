package assetmerge

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const projectedName = "projected.sqlite"

// Keep a rooted handle to the original directory. SQLite can unlink its own
// sidecars on close, so their names alone never establish cleanup ownership.
type catalogScratch struct {
	parent       *area
	dir          *area
	name         string
	files        map[string]scratchFile
	catalogMoved bool
}

type scratchFile struct {
	identity string
	pin      *os.File
}

func newScratch(parent *area) (*catalogScratch, error) {
	name := ".qimeng-merge-" + rand.Text()
	if err := parent.mkdir(name); err != nil {
		return nil, phase("private_scratch", err)
	}
	root, err := parent.root.OpenRoot(name)
	if err != nil {
		return nil, phase("private_scratch_cleanup_required", err)
	}
	info, err := root.Stat(".")
	if err != nil {
		return nil, phase("private_scratch_cleanup_required", errors.Join(err, root.Close()))
	}
	dir := &area{filepath.Join(parent.name, name), root, info}
	if err := dir.bound(); err != nil {
		return nil, phase("private_scratch_cleanup_required", errors.Join(err, root.Close()))
	}
	return &catalogScratch{parent: parent, dir: dir, name: name, files: make(map[string]scratchFile)}, nil
}

func (s *catalogScratch) create(name string) (*os.File, error) {
	f, err := s.dir.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	key, err := scratchFileIdentity(f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if old := s.files[name]; old.pin != nil {
		if err := old.pin.Close(); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
	pin, err := pinScratchFile(f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	s.files[name] = scratchFile{key, pin}
	return f, nil
}

func (s *catalogScratch) copyCatalog(ctx context.Context, from string, reserve int64) (stamp, error) {
	f, err := s.create(projectedName)
	if err != nil {
		return stamp{}, err
	}
	st, err := s.parent.readFile(ctx, from, &reservedWriter{ctx: ctx, a: s.parent, reserve: reserve, out: f})
	if err == nil {
		err = f.Sync()
	}
	return st, errors.Join(err, f.Close())
}

func (s *catalogScratch) known(name string, owned scratchFile) error {
	if owned.identity == "" {
		return errors.New("unrecognized entry in private catalog scratch")
	}
	f, _, err := s.dir.openRegular(name)
	if err != nil {
		return err
	}
	id, err := scratchFileIdentity(f)
	if err == nil && id != owned.identity {
		err = errors.New("private scratch file identity changed")
	}
	return errors.Join(err, f.Close())
}

func (s *catalogScratch) reserveSidecars() error {
	if err := s.dir.bound(); err != nil {
		return err
	}
	if err := s.known(projectedName, s.files[projectedName]); err != nil {
		return err
	}
	// Reserve names by exclusive creation before giving SQLite the path. It
	// may consume these empty files. A replacement is never adopted by name.
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		name := projectedName + suffix
		_, err := s.dir.root.Lstat(name)
		if err == nil {
			if err := s.known(name, s.files[name]); err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		f, err := s.create(name)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *catalogScratch) cleanup() (err error) {
	defer func() {
		for _, file := range s.files {
			if file.pin != nil {
				err = errors.Join(err, file.pin.Close())
			}
		}
		err = errors.Join(err, s.dir.root.Close())
	}()
	if err := s.dir.bound(); err != nil {
		return err
	}
	view, err := s.dir.root.Open(".")
	if err != nil {
		return err
	}
	// Inspect at most the four explicitly reserved entries plus one outsider.
	names, readErr := view.Readdirnames(5)
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, view.Close()); err != nil {
		return err
	}
	present := map[string]os.FileInfo{}
	for _, name := range names {
		info, err := s.dir.root.Lstat(name)
		if err != nil {
			return err
		}
		if err := s.known(name, s.files[name]); err != nil {
			return err
		}
		present[name] = info
	}
	_, catalogPresent := present[projectedName]
	if s.files[projectedName].identity != "" && catalogPresent == s.catalogMoved {
		return errors.New("unexpected private catalog location")
	}
	// Claim an unpredictable, no-replace name before unlinking. A long-lived
	// scratch basename that was reassigned cannot designate the removal target.
	quarantine := ".qimeng-merge-clean-" + rand.Text()
	if err := s.parent.move(s.name, quarantine, s.dir.info); err != nil {
		return err
	}
	s.dir.name = filepath.Join(s.parent.name, quarantine)
	for name, info := range present {
		target := ".remove-" + rand.Text()
		if err := s.dir.move(name, target, info); err != nil {
			return err
		}
		if err := s.known(target, s.files[name]); err != nil {
			return err
		}
		if err := s.dir.root.Remove(target); err != nil {
			return err
		}
	}
	if err := s.dir.bound(); err != nil {
		return err
	}
	// RemoveDirectory/unlinkat(AT_REMOVEDIR) refuse files and nonempty folders.
	return removeScratchDirectory(s.parent, quarantine)
}

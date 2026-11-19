package service

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"

	"qimeng.local/next/server/internal/model"
)

type selectedImageReads struct {
	slots chan string
	drain sync.Once
}

func (p *selectedImageReads) init() {
	p.slots = make(chan string, 2)
	for i := range cap(p.slots) {
		p.slots <- "cache/selected-image-reads/" + strconv.Itoa(i)
	}
}

// stopWork wakes queued reads and closes HTTP readers. Keep the data root
// and its exclusive owner until every checked-out scratch file is released.
func (p *selectedImageReads) wait() {
	p.drain.Do(func() {
		for range cap(p.slots) {
			<-p.slots
		}
	})
}

type selectedImageSnapshot struct {
	*os.File
	name  string
	close func() error
}

func (f *selectedImageSnapshot) Close() error { return f.close() }

func (s *Service) snapshotSelectedImage(ctx context.Context, id string, version int64, httpReader bool) (*selectedImageSnapshot, model.Asset, error) {
	var name string
	select {
	case name = <-s.selectedReads.slots:
	case <-ctx.Done():
		return nil, model.Asset{}, ctx.Err()
	case <-s.workCtx.Done():
		return nil, model.Asset{}, s.workCtx.Err()
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.workCtx, cancel)
	release := func() {
		stop()
		cancel()
		s.selectedReads.slots <- name
	}
	file, a, err := s.copySelectedImage(ctx, id, version, name)
	if err != nil {
		release()
		return nil, a, err
	}
	closeFile := sync.OnceValue(func() error {
		err := errors.Join(file.Close(), s.Root.Remove(name))
		release()
		return err
	})
	closeReader := closeFile
	if httpReader {
		// A disconnected client can still be blocked in a network write. A
		// decoder instead owns the file until its reads or child process end.
		stopClose := context.AfterFunc(ctx, func() { closeFile() })
		closeReader = func() error {
			stopClose()
			return closeFile()
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, a, errors.Join(err, closeReader())
	}
	return &selectedImageSnapshot{File: file, name: name, close: closeReader}, a, nil
}

func (s *Service) copySelectedImage(ctx context.Context, id string, version int64, name string) (*os.File, model.Asset, error) {
	// Both kinds of verified copy use the private data volume. Serialize
	// their space checks and writes, releasing this slot before transmission.
	select {
	case s.protectionRead <- struct{}{}:
		defer func() { <-s.protectionRead }()
	case <-ctx.Done():
		return nil, model.Asset{}, ctx.Err()
	}
	source, a, err := s.openContent(ctx, id, version, true)
	if err != nil {
		return nil, a, err
	}
	defer source.Close()
	file, err := s.stageSelectedImage(ctx, source, a, name)
	return file, a, err
}

func (s *Service) stageSelectedImage(ctx context.Context, source *os.File, a model.Asset, name string) (_ *os.File, err error) {
	changed := func() error {
		return Fail(409, "content_mismatch", "媒体原件的长度或摘要已变化。")
	}
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != a.Size {
		return nil, changed()
	}
	if err = s.ensureParent(name); err != nil {
		return nil, err
	}
	// Ownership of this fixed slot survives a hard stop. Drop only its
	// regular scratch file, before checking space or creating a new object.
	if old, err := s.Root.Lstat(name); err == nil {
		if !old.Mode().IsRegular() || syncPathLink(old) {
			return nil, Fail(409, "snapshot_scratch_occupied", "图片暂存位置被其他类型的对象占用。")
		}
		if err = s.Root.Remove(name); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	noSpace := func() error {
		return Fail(507, "insufficient_storage", "剩余空间不足以暂存并校验图片原件。")
	}
	free, err := freeBytes(s.Config.DataDir)
	if err != nil {
		return nil, err
	}
	if a.Size > free-s.Config.ReserveBytes {
		return nil, noSpace()
	}
	out, err := s.Root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Root.Remove(name))
		}
	}()
	writer := &reserveWriter{ctx: ctx, s: s, file: out}
	hash, n, copyErr := hashReaderContext(ctx, io.TeeReader(io.LimitReader(source, a.Size), writer), nil)
	var space *Error
	if errors.As(copyErr, &space) && space.Code == "insufficient_storage" {
		copyErr = noSpace()
	}
	if err = errors.Join(copyErr, out.Close()); err != nil {
		return nil, err
	}
	info, err = source.Stat()
	if err != nil {
		return nil, err
	}
	if n != a.Size || hash != a.Hash || info.Size() != a.Size {
		return nil, changed()
	}
	return s.Root.Open(name)
}

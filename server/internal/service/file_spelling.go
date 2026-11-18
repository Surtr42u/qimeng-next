package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
)

// A case-insensitive volume can resolve two spellings to one directory entry.
// SameFile also returns true for two real hard links, so it cannot authorize
// removing either name on its own.
func (s *Service) exactNames(directory, source, target string) (bool, bool, error) {
	folder, err := s.Root.Open(directory)
	if err != nil {
		return false, false, err
	}
	defer folder.Close()
	var sourceFound, targetFound bool
	for {
		names, err := folder.Readdirnames(256)
		for _, name := range names {
			sourceFound = sourceFound || name == source
			targetFound = targetFound || name == target
		}
		if errors.Is(err, io.EOF) || sourceFound && targetFound {
			return sourceFound, targetFound, nil
		}
		if err != nil {
			return false, false, err
		}
	}
}

// classifyMove runs before the intent is written. Normal target conflicts
// leave no intent for startup recovery to retry.
func (s *Service) classifyMove(source, target string) (bool, error) {
	if err := s.safe(target); err != nil {
		return false, err
	}
	sourceDir, targetDir := path.Dir(source), path.Dir(target)
	if sourceDir != targetDir {
		oldDir, err := s.Root.Stat(sourceDir)
		if err != nil {
			return false, err
		}
		newDir, err := s.Root.Stat(targetDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if err == nil && os.SameFile(oldDir, newDir) {
			return false, Fail(409, "directory_spelling_conflict", "目标目录的实际名称与输入大小写不同。请使用现有目录的名称。")
		}
	}
	targetInfo, err := s.Root.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sourceInfo, err := s.Root.Lstat(source)
	if err != nil {
		return false, err
	}
	if sourceDir == targetDir && os.SameFile(sourceInfo, targetInfo) {
		foundSource, foundTarget, err := s.exactNames(sourceDir, path.Base(source), path.Base(target))
		if err != nil {
			return false, err
		}
		if foundSource != foundTarget {
			return true, nil
		}
	}
	return false, Fail(409, "path_occupied", "目标已有文件，请选择其他名称。")
}

func spellingHold(j fileIntent) string {
	return path.Join("libraries", j.After.LibraryID, ".qimeng-internal", "rename-holds", j.ID)
}

// Upgrade an unfinished move written before the distinction between directory
// entries and hard links was recorded. A committed legacy row still needs its
// on-disk name repaired before cleanup may remove anything.
func (s *Service) recoverMoveSpelling(ctx context.Context, j fileIntent) (fileIntent, error) {
	if j.Kind != "move" || j.SpellingOnly || j.Source == j.Target {
		return j, nil
	}
	source, err := s.Root.Lstat(j.Source)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	target, err := s.Root.Lstat(j.Target)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil || !os.SameFile(source, target) {
		return j, err
	}
	sourceDir, targetDir := path.Dir(j.Source), path.Dir(j.Target)
	if sourceDir != targetDir {
		first, err := s.Root.Stat(sourceDir)
		if err != nil {
			return j, err
		}
		second, err := s.Root.Stat(targetDir)
		if err != nil {
			return j, err
		}
		if !os.SameFile(first, second) {
			return j, nil
		}
	}
	foundSource, foundTarget, err := s.exactNames(targetDir, path.Base(j.Source), path.Base(j.Target))
	if err != nil {
		return j, err
	}
	if foundSource && foundTarget && path.Base(j.Source) != path.Base(j.Target) {
		return j, nil
	}
	if sourceDir != targetDir {
		return j, Fail(409, "directory_spelling_conflict", "待恢复移动的两个目录名称指向同一目录。原件已保留，请核对登记。")
	}
	j.SpellingOnly = true
	if err := s.saveIntent(ctx, j); err != nil {
		return j, err
	}
	return j, nil
}

// Keep a durable hard link on the same volume until the exact new directory
// entry exists and the catalog transaction commits. A retry can resume with
// only the hold, or with the new entry already linked.
func (s *Service) publishSpelling(j fileIntent) error {
	if path.Dir(j.Source) != path.Dir(j.Target) || path.Base(j.Source) == path.Base(j.Target) {
		return Fail(409, "directory_spelling_conflict", "请先核对原件的实际目录名称。")
	}
	sourceExists, targetExists, err := s.exactNames(path.Dir(j.Source), path.Base(j.Source), path.Base(j.Target))
	if err != nil {
		return err
	}
	hold := spellingHold(j)
	if sourceExists && targetExists {
		return Fail(409, "path_occupied", "原名称和目标名称都存在，请核对两个文件。")
	}
	if targetExists {
		if err := s.verifyFileIdentity(j.Target, j.SourceObject); err != nil {
			return err
		}
		if err := s.verify(j.Target, j.After.Hash, j.After.Size); err != nil {
			return err
		}
		// The old spelling may already have been fixed by a recovered request.
		// If a hold exists, byte equality alone cannot substitute for identity.
		held, err := s.Root.Lstat(hold)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := s.Root.Lstat(j.Target)
		if err != nil {
			return err
		}
		if !os.SameFile(held, target) {
			return Fail(409, "path_occupied", "目标文件与待恢复原件身份不同。")
		}
		return nil
	}
	if sourceExists {
		if err := s.verifyFileIdentity(j.Source, j.SourceObject); err != nil {
			return err
		}
		if err := s.verify(j.Source, j.After.Hash, j.After.Size); err != nil {
			return err
		}
		if err := s.linkNoReplace(j.Source, hold); err != nil {
			return err
		}
		if err := s.verify(hold, j.After.Hash, j.After.Size); err != nil {
			return err
		}
		if err := s.removeFileObject(j.Source, j.SourceObject, j.After.Hash, j.After.Size); err != nil {
			return err
		}
	} else if err := s.verify(hold, j.After.Hash, j.After.Size); err != nil {
		return err
	}
	if err := s.verifyFileIdentity(hold, j.SourceObject); err != nil {
		return err
	}
	if err := s.linkNoReplace(hold, j.Target); err != nil {
		return err
	}
	return s.verify(j.Target, j.After.Hash, j.After.Size)
}

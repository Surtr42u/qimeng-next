package service

import "context"

// The digest covers the entire original. Native image probes seek within
// that same object, including JPEG orientation. Video probes read the
// container header there too. Linux and Android pass that descriptor to the
// probe tool. No descriptor is kept in the write batch.
func (s *Service) inspectScanOriginal(ctx context.Context, run *scanRun, file *scannedFile, skipHash string) string {
	if err := s.safe(file.full); err != nil {
		return "read_failed"
	}
	original, err := s.Root.Open(file.full)
	if err != nil {
		return "read_failed"
	}
	defer original.Close()
	info, err := original.Stat()
	if err != nil {
		return "read_failed"
	}
	if !file.sameOriginal(info) {
		return "source_changed"
	}
	file.initial = info
	if run.hashBuffer == nil {
		run.hashBuffer = make([]byte, 32<<10)
	}
	file.hash, file.size, err = hashReaderContext(ctx, original, run.hashBuffer)
	if err != nil {
		return "read_failed"
	}
	if file.size != info.Size() {
		return "source_changed"
	}
	if file.hash == skipHash {
		// A write notice still checks the live name before reporting a skip.
		stable, err := s.scanOriginalStable(*file)
		if err != nil {
			return "read_failed"
		}
		if !stable {
			return "source_changed"
		}
		return ""
	}
	input := mediaInput{name: file.full}
	if builtinImage(file.probe.MimeType) || file.kind == "video" {
		input.file, input.size = original, file.size
	}
	if err = s.probeMedia(ctx, input, &file.probe); err != nil {
		return "invalid_media"
	}
	return ""
}

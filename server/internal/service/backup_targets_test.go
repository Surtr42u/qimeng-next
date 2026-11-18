package service

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestIndependentObjectCopyReadsBackAndNeverOverwritesCorruptEvidence(t *testing.T) {
	directory := t.TempDir()
	root, e := os.OpenRoot(directory)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	body := "synthetic source bytes"
	hash := digest([]byte(body))
	opened := 0
	open := func() (io.ReadCloser, error) { opened++; return io.NopCloser(strings.NewReader(body)), nil }
	if e = copyTargetObject(context.Background(), root, directory, "temporary/work.partial", "objects/content", hash, int64(len(body)), 1, open); e != nil {
		t.Fatal(e)
	}
	if opened != 1 {
		t.Fatal("source was not streamed")
	}
	if e = copyTargetObject(context.Background(), root, directory, "temporary/work.partial", "objects/content", hash, int64(len(body)), 1, open); e != nil {
		t.Fatal(e)
	}
	if opened != 1 {
		t.Fatal("identical protected content was copied twice")
	}
	damaged, e := root.OpenFile("objects/content", os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = damaged.WriteString(strings.Repeat("x", len(body))); e != nil {
		t.Fatal(e)
	}
	damaged.Close()
	if e = copyTargetObject(context.Background(), root, directory, "temporary/work.partial", "objects/content", hash, int64(len(body)), 1, open); e == nil {
		t.Fatal("changed backup object was blessed or overwritten")
	}
	if opened != 1 {
		t.Fatal("corrupt evidence was silently replaced")
	}
}

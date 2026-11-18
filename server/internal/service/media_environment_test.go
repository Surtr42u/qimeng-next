package service

import (
	"os"
	"os/exec"
	"testing"
)

func TestMediaChildDoesNotInheritCredentials(t *testing.T) {
	t.Setenv("QIMENG_AI_TOKEN", "synthetic-secret")
	t.Setenv("HTTP_PROXY", "synthetic-proxy-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-cloud-secret")
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, "-test.run=^TestMediaEnvironmentChild$", "--", "media-env-child")
	configureMediaProcess(cmd)
	if e := cmd.Run(); e != nil {
		t.Fatalf("isolated child failed: %v", e)
	}
}

func TestMediaEnvironmentChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "media-env-child" {
		return
	}
	for _, name := range []string{"QIMENG_AI_TOKEN", "HTTP_PROXY", "AWS_SECRET_ACCESS_KEY"} {
		if os.Getenv(name) != "" {
			t.Fatal("child inherited a private environment variable")
		}
	}
}

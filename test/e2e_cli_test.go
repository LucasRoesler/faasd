package test

import (
	"strings"
	"testing"
	"time"

	exec "github.com/alexellis/go-execute/pkg/v1"
)

func TestCLIWorkflow(t *testing.T) {
	steps := []struct {
		name           string
		cmd            exec.ExecTask
		code           int
		stdOutContains string
		wait           time.Duration
	}{
		{
			name: "logs",
			cmd: exec.ExecTask{
				Command: "/usr/local/bin/faas-cli",
				Args:    []string{"logs", "figlet", "--since=15m", "--follow=false"},
				Shell:   true,
			},
			stdOutContains: "Forking",
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			res, err := step.cmd.Execute()
			if err != nil {
				t.Fatalf(`unexpected error "%s %v":\n%s`, step.cmd.Command, step.cmd.Args, err)
			}

			if step.stdOutContains != "" && !strings.Contains(res.Stdout, step.stdOutContains) {
				t.Fatalf("expected command output to contain:\n%s\ngot:\n%s", step.stdOutContains, res.Stdout)
			}

			if res.ExitCode != step.code {
				t.Logf("stdout: %s\nstderr: %s", res.Stdout, res.Stderr)
				t.Fatalf("expected exit code %d, got %d", step.code, res.ExitCode)
			}

		})
	}

}

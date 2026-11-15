package cli

import (
	"io"
	"testing"

	"github.com/mclucy/lucy/terminal/pager"
	"github.com/spf13/cobra"
)

func TestPagerFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    pager.Mode
		wantErr bool
	}{
		{name: "default", want: pager.Auto},
		{name: "bare", args: []string{"--pager"}, want: pager.Always},
		{name: "auto", args: []string{"--pager=auto"}, want: pager.Auto},
		{name: "never", args: []string{"--pager=never"}, want: pager.Never},
		{name: "invalid", args: []string{"--pager=sometimes"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			invocation := &Invocation{Pager: pager.Auto}
			cmd := &cobra.Command{Use: "lucy", RunE: func(*cobra.Command, []string) error {
				return nil
			}}
			AddPagerFlag(cmd, invocation)
			cmd.SetArgs(test.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if test.wantErr {
				if err == nil {
					t.Fatal("Execute accepted an unknown pager mode")
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if invocation.Pager != test.want {
				t.Fatalf("pager mode = %v, want %v", invocation.Pager, test.want)
			}
		})
	}
}

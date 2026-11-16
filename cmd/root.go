package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/spf13/cobra"
)

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}

func jsonMode(c *cobra.Command) bool {
	v, _ := c.Flags().GetBool("json")
	return v
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "telegram-cli",
		Short:         "Telegram client for the command line",
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			warns, err := config.TightenPermissions()
			for _, w := range warns {
				fmt.Fprintln(c.ErrOrStderr(), "warning:", w)
			}
			if err != nil {
				return err
			}
			_, err = config.LoadConfig() // surface a bad config.toml early (exit 2)
			return err
		},
	}
	root.PersistentFlags().Bool("json", false, "print JSON instead of readable text")
	root.SetVersionTemplate("telegram-cli {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &config.UsageError{Msg: err.Error()}
	})
	root.AddCommand(newInitCmd())
	return root
}

// exitCode maps an error to its exit code and error.code string.
func exitCode(err error) (int, string) {
	var usage *config.UsageError
	var nc *config.NotConfiguredError
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, context.Canceled):
		return 130, "INTERRUPTED"
	case errors.As(err, &usage), strings.HasPrefix(err.Error(), "unknown command"):
		return 2, "USAGE"
	case errors.As(err, &nc):
		return 3, "NOT_CONFIGURED"
	}
	return 1, "ERROR"
}

// run executes root, prints any error to errW, and returns the exit code.
func run(ctx context.Context, root *cobra.Command, errW io.Writer) int {
	err := root.ExecuteContext(ctx)
	code, name := exitCode(err)
	if err != nil {
		asJSON, _ := root.PersistentFlags().GetBool("json")
		output.Error(errW, asJSON, name, err.Error())
	}
	return code
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := run(ctx, newRootCmd(), os.Stderr)
	stop()
	os.Exit(code)
}

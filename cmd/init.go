package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newInitCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Save the Telegram API ID and hash",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runInit(c, force)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite existing credentials")
	return c
}

func runInit(c *cobra.Command, force bool) error {
	credPath, err := config.CredentialsPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(credPath); err == nil && !force {
		return &config.UsageError{Msg: fmt.Sprintf("%s already exists; use --force to overwrite", credPath)}
	}

	in := c.InOrStdin()
	errW := c.ErrOrStderr()
	br := bufio.NewReader(in)

	fmt.Fprint(errW, "API ID: ")
	idStr, err := readLine(br)
	if err != nil {
		return err
	}
	fmt.Fprint(errW, "API hash: ")
	var hash string
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(errW)
		if err != nil {
			return err
		}
		hash = string(b)
	} else if hash, err = readLine(br); err != nil {
		return err
	}

	id, err := config.ParseAPIID(idStr)
	if err != nil {
		return err
	}
	if _, err := config.SaveCredentials(config.Credentials{APIID: id, APIHash: strings.TrimSpace(hash)}); err != nil {
		return err
	}

	cfgPath, err := config.ConfigPath()
	if err != nil {
		return err
	}
	created := false
	if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) {
		if err := config.WriteFileAtomic(cfgPath, []byte(config.DefaultConfigTOML)); err != nil {
			return err
		}
		created = true
	} else if err != nil {
		return err
	}

	out := c.OutOrStdout()
	if jsonMode(c) {
		return output.JSON(out, map[string]any{"credentials": credPath, "config": cfgPath, "config_created": created})
	}
	fmt.Fprintf(out, "Saved credentials to %s\n", credPath)
	if created {
		fmt.Fprintf(out, "Wrote default config to %s\n", cfgPath)
	} else {
		fmt.Fprintf(out, "Config already exists at %s\n", cfgPath)
	}
	return nil
}

func readLine(br *bufio.Reader) (string, error) {
	s, err := br.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && s != "") {
		return "", &config.UsageError{Msg: "unexpected end of input"}
	}
	return strings.TrimSpace(s), nil
}

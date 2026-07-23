package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/keyring"
)

func newRemoveKdbxCmd() *cobra.Command {
	var configPath string
	var deleteFile, forgetPassword bool
	cmd := &cobra.Command{
		Use:   "remove-kdbx <name>",
		Short: "Remove a local kdbx source from the config",
		Long: "Remove a local kdbx source from the config. On a terminal you are asked " +
			"whether to also delete the kdbx file itself (off by default — harmos is " +
			"otherwise read-only) and whether to remove its saved keyring password. Use " +
			"--delete-file / --forget-password to answer non-interactively.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfigAt(configPath)
			if err != nil {
				return err
			}
			p := cfg.Profile(name)
			if p == nil {
				return fmt.Errorf("no profile named %q", name)
			}
			if p.Type != config.Kdbx {
				return fmt.Errorf("%q is a %s source; remove-kdbx only removes local kdbx sources", name, p.Type)
			}

			if !cmd.Flags().Changed("delete-file") && onTTY() {
				ok, err := confirm(fmt.Sprintf("also delete the kdbx file %s? [y/N] ", p.Path))
				if err != nil {
					return err
				}
				deleteFile = ok
			}
			if !cmd.Flags().Changed("forget-password") && onTTY() {
				ok, err := confirm(fmt.Sprintf("also remove %q's saved password from the keyring? [y/N] ", name))
				if err != nil {
					return err
				}
				forgetPassword = ok
			}
			return runRemoveKdbx(configPath, name, deleteFile, forgetPassword, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&deleteFile, "delete-file", false, "also delete the kdbx file from disk")
	cmd.Flags().BoolVar(&forgetPassword, "forget-password", false, "also remove the saved keyring password")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runRemoveKdbx(configPath, name string, deleteFile, forgetPassword bool, out io.Writer) error {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	p := cfg.Profile(name)
	if p == nil {
		return fmt.Errorf("no profile named %q", name)
	}
	if p.Type != config.Kdbx {
		return fmt.Errorf("%q is a %s source; remove-kdbx only removes local kdbx sources", name, p.Type)
	}
	kdbxPath := p.Path
	remaining := len(cfg.Profiles) - 1

	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	next, ok := removeProfileBlock(string(content), name)
	if !ok {
		return fmt.Errorf("could not locate the %q profile block in %s", name, configPath)
	}
	if err := writeFileAtomic(configPath, []byte(next)); err != nil {
		return err
	}
	if remaining >= 1 {
		if _, err := config.Load(configPath); err != nil {
			return fmt.Errorf("config is invalid after removing %q: %w", name, err)
		}
	}
	emitf(out, "removed source %q from %s\n", name, configPath)
	if remaining == 0 {
		emitf(out, "note: the config now has no sources\n")
	}

	if deleteFile {
		if err := os.Remove(kdbxPath); err != nil {
			emitf(out, "warning: could not delete the kdbx file: %v\n", err)
		} else {
			emitf(out, "deleted the kdbx file %s\n", kdbxPath)
		}
	}
	if forgetPassword {
		if err := keyring.Forget(name); err != nil {
			emitf(out, "warning: could not remove the keyring password: %v\n", err)
		} else {
			emitf(out, "removed %q's saved password from the keyring\n", name)
		}
	}
	return nil
}

func newRemovePasswordCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove-password <name[,name...]|all>",
		Short: "Remove saved passwords from the OS keyring",
		Long: "Remove one or more sources' saved passwords from the OS keyring. Pass a " +
			"comma-separated list of profile names, or `all` for every configured " +
			"source. A Pleasant source removes the shared master password.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemovePassword(configPath, args[0], cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runRemovePassword(configPath, spec string, out io.Writer) error {
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return err
	}

	var names []string
	for t := range strings.SplitSeq(spec, ",") {
		t = strings.TrimSpace(t)
		switch t {
		case "":
			continue
		case "all":
			names = names[:0]
			for _, p := range cfg.Profiles {
				names = append(names, p.Name)
			}
		default:
			names = append(names, t)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no source names given")
	}

	masterDone := false
	for _, name := range names {
		p := cfg.Profile(name)
		if p == nil {
			return fmt.Errorf("no profile named %q", name)
		}
		if p.Type == config.Pleasant {
			if masterDone {
				continue
			}
			if err := keyring.ForgetMaster(); err != nil {
				return err
			}
			masterDone = true
			emitf(out, "removed the shared master password from the keyring\n")
			continue
		}
		if err := keyring.Forget(name); err != nil {
			return err
		}
		emitf(out, "removed %q's saved password from the keyring\n", name)
	}
	return nil
}

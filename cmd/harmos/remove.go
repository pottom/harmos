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

func newRemoveSourceCmd() *cobra.Command {
	var configPath string
	var deleteFile, forgetPassword bool
	cmd := &cobra.Command{
		Use:     "remove-source <name>",
		Aliases: []string{"remove-kdbx"},
		Short:   "Remove a source from the config",
		Long: "Remove a source from the config. On a terminal you are asked whether to " +
			"also delete the local file (the kdbx, or a Pleasant cache — off by default, " +
			"harmos never writes to it unless you unlock it) and whether to remove its saved keyring " +
			"password. Use --delete-file / --forget-password to answer non-interactively.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfigAt(configPath)
			if err != nil {
				return err
			}
			p := cfg.Source(name)
			if p == nil {
				return fmt.Errorf("no source named %q", name)
			}
			localFile := p.Path
			if p.Type == config.Pleasant {
				localFile = p.Cache
			}

			if !cmd.Flags().Changed("delete-file") && onTTY() {
				ok, err := confirm(fmt.Sprintf("also delete the file %s? [y/N] ", localFile))
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
			return runRemoveSource(configPath, name, deleteFile, forgetPassword, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&deleteFile, "delete-file", false, "also delete the source's local file from disk")
	cmd.Flags().BoolVar(&forgetPassword, "forget-password", false, "also remove the saved keyring password")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runRemoveSource(configPath, name string, deleteFile, forgetPassword bool, out io.Writer) error {
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
	p := cfg.Source(name)
	if p == nil {
		return fmt.Errorf("no source named %q", name)
	}
	localFile := p.Path
	if p.Type == config.Pleasant {
		localFile = p.Cache
	}
	isPleasant := p.Type == config.Pleasant
	otherPleasant := 0
	for _, q := range cfg.Sources {
		if q.Name != name && q.Type == config.Pleasant {
			otherPleasant++
		}
	}

	remaining, err := config.RemoveSource(configPath, name)
	if err != nil {
		return err
	}
	emitf(out, "removed source %q from %s\n", name, configPath)
	if remaining == 0 {
		emitf(out, "note: the config now has no sources\n")
	}

	if deleteFile {
		if err := os.Remove(localFile); err != nil {
			emitf(out, "warning: could not delete the file: %v\n", err)
		} else {
			emitf(out, "deleted the file %s\n", localFile)
		}
	}
	if forgetPassword {
		forgetSourcePassword(out, name, isPleasant, otherPleasant)
	}
	return nil
}

// forgetSourcePassword removes a removed source's keyring password: a kdbx source
// clears its own entry; a Pleasant source clears the shared master only when it
// was the last Pleasant source (other Pleasant sources still need it).
func forgetSourcePassword(out io.Writer, name string, isPleasant bool, otherPleasant int) {
	if isPleasant {
		if otherPleasant > 0 {
			emitf(out, "kept the shared master password (other Pleasant sources still use it)\n")
			return
		}
		if err := keyring.ForgetMaster(); err != nil {
			emitf(out, "warning: could not remove the master password: %v\n", err)
		} else {
			emitf(out, "removed the shared master password from the keyring\n")
		}
		return
	}
	if err := keyring.Forget(name); err != nil {
		emitf(out, "warning: could not remove the keyring password: %v\n", err)
	} else {
		emitf(out, "removed %q's saved password from the keyring\n", name)
	}
}

func newRemovePasswordCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "remove-password <name[,name...]|all>",
		Short: "Remove saved passwords from the OS keyring",
		Long: "Remove one or more sources' saved passwords from the OS keyring. Pass a " +
			"comma-separated list of source names, or `all` for every configured " +
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
			for _, p := range cfg.Sources {
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
		p := cfg.Source(name)
		if p == nil {
			return fmt.Errorf("no source named %q", name)
		}
		if p.Type == config.Pleasant {
			if err := keyring.ForgetServer(name); err != nil {
				return err
			}
			emitf(out, "removed %q's saved server password from the keyring\n", name)
			if !masterDone {
				if err := keyring.ForgetMaster(); err != nil {
					return err
				}
				masterDone = true
				emitf(out, "removed the shared master password from the keyring\n")
			}
			continue
		}
		if err := keyring.Forget(name); err != nil {
			return err
		}
		emitf(out, "removed %q's saved password from the keyring\n", name)
	}
	return nil
}

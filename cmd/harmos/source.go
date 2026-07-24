package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/config"
)

// confirm asks a yes/no question on the terminal, defaulting to no.
func confirm(label string) (bool, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func newSourcesCmd() *cobra.Command {
	var configPath string
	var noHeaders bool
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "List configured sources (no unlock needed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSources(configPath, !noHeaders, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&noHeaders, "no-headers", false, "omit the header row")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runSources(configPath string, showHeaders bool, out io.Writer) error {
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return err
	}

	anyKeyfile := false
	for _, p := range cfg.Profiles {
		if p.Keyfile != "" {
			anyKeyfile = true
			break
		}
	}
	headers := []string{"NAME", "TYPE", "LOCATION"}
	if anyKeyfile {
		headers = append(headers, "KEYFILE")
	}

	rows := make([][]string, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		row := []string{p.Name, string(p.Type), loc}
		if anyKeyfile {
			kf := p.Keyfile
			if kf == "" {
				kf = "-"
			}
			row = append(row, kf)
		}
		rows = append(rows, row)
	}
	printTable(out, headers, rows, showHeaders)
	return nil
}

func newAddSourceCmd() *cobra.Command {
	var srcType, name, keyfile, configPath string
	var url, user, cache, caBundle string
	var force, savePassword bool
	cmd := &cobra.Command{
		Use:     "add-source [path]",
		Aliases: []string{"add-kdbx"},
		Short:   "Register a source (local kdbx by default, or --type pps)",
		Long: "Add a source to the config. By default it is a local KeePass .kdbx file — " +
			"pass its path; harmos never writes to that file. With --type pps it is a " +
			"Pleasant Password Server (--url, --user; --cache defaults to " +
			"$XDG_DATA_HOME/harmos/<name>.kdbx); run `harmos sync` afterwards to populate " +
			"its cache. --save-password stores the password (for Pleasant, the shared " +
			"master) in the OS keyring.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch srcType {
			case "", "kdbx":
				if len(args) != 1 {
					return fmt.Errorf("add-source needs the path to a .kdbx file")
				}
				derived := name
				if derived == "" {
					derived = config.DeriveProfileName(args[0])
				}
				if err := runAddSource(configPath, args[0], name, keyfile, force, out); err != nil {
					return err
				}
				if savePassword {
					return savePasswordFor(derived, out)
				}
				return nil
			case "pps", "pleasant":
				if len(args) != 0 {
					return fmt.Errorf("a Pleasant source takes no path; use --url/--user")
				}
				if err := runAddPleasant(configPath, name, url, user, cache, caBundle, force, out); err != nil {
					return err
				}
				if savePassword {
					return saveMaster(out)
				}
				return nil
			default:
				return fmt.Errorf("unknown --type %q (want kdbx or pps)", srcType)
			}
		},
	}
	cmd.Flags().StringVar(&srcType, "type", "", "source type: kdbx (default) or pps")
	cmd.Flags().StringVar(&name, "name", "", "profile name (default: derived from the path or cache)")
	cmd.Flags().StringVar(&keyfile, "keyfile", "", "kdbx: key file, if the file uses one")
	cmd.Flags().StringVar(&url, "url", "", "pps: server URL")
	cmd.Flags().StringVar(&user, "user", "", "pps: server username")
	cmd.Flags().StringVar(&cache, "cache", "", "pps: local cache kdbx path (default: $XDG_DATA_HOME/harmos/<name>.kdbx)")
	cmd.Flags().StringVar(&caBundle, "ca-bundle", "", "pps: CA bundle for a private CA")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing profile of the same name without asking")
	cmd.Flags().BoolVar(&savePassword, "save-password", false, "prompt for the password and store it in the OS keyring")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runAddSource(configPath, path, name, keyfile string, force bool, out io.Writer) error {
	cfgPath, err := configPathOrDefault(configPath)
	if err != nil {
		return err
	}
	kdbxPath, err := config.AbsPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(kdbxPath)
	if err != nil {
		return fmt.Errorf("kdbx file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, not a .kdbx file", kdbxPath)
	}
	if keyfile != "" {
		if keyfile, err = config.AbsPath(keyfile); err != nil {
			return err
		}
		if _, err := os.Stat(keyfile); err != nil {
			return fmt.Errorf("keyfile: %w", err)
		}
	}

	if name == "" {
		name = config.DeriveProfileName(kdbxPath)
	}
	if name == "" {
		return fmt.Errorf("could not derive a profile name from %q; pass --name", path)
	}

	proceed, overwrite, err := confirmOverwrite(cfgPath, name, force, out)
	if err != nil || !proceed {
		return err
	}
	verb, err := config.WriteKdbxProfile(cfgPath, name, kdbxPath, keyfile, overwrite)
	if err != nil {
		return err
	}
	emitf(out, "%s kdbx source %q\n", verb, name)
	emitf(out, "  path     %s\n", kdbxPath)
	if keyfile != "" {
		emitf(out, "  keyfile  %s\n", keyfile)
	}
	emitf(out, "next: run `harmos` to browse it (you'll be asked for its password)\n")
	return nil
}

func runAddPleasant(configPath, name, url, user, cache, caBundle string, force bool, out io.Writer) error {
	cfgPath, err := configPathOrDefault(configPath)
	if err != nil {
		return err
	}
	if url == "" || user == "" {
		return fmt.Errorf("a Pleasant source needs --url and --user")
	}
	cacheDefaulted := cache == ""
	if cacheDefaulted {
		if name == "" {
			return fmt.Errorf("a Pleasant source needs --name (or --cache) so its cache can be named")
		}
		p, derr := config.DefaultCachePath(name)
		if derr != nil {
			return derr
		}
		cache = p
	}
	cachePath, err := config.AbsPath(cache)
	if err != nil {
		return err
	}
	if caBundle != "" {
		if caBundle, err = config.AbsPath(caBundle); err != nil {
			return err
		}
		if _, err := os.Stat(caBundle); err != nil {
			return fmt.Errorf("ca-bundle: %w", err)
		}
	}
	if name == "" {
		name = config.DeriveProfileName(cachePath)
	}
	if name == "" {
		return fmt.Errorf("could not derive a profile name; pass --name")
	}

	// Make sure the cache directory exists so `harmos sync` can write into it.
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	proceed, overwrite, err := confirmOverwrite(cfgPath, name, force, out)
	if err != nil || !proceed {
		return err
	}
	verb, err := config.WritePleasantProfile(cfgPath, name, url, user, cachePath, caBundle, overwrite)
	if err != nil {
		return err
	}
	emitf(out, "%s pps source %q\n", verb, name)
	emitf(out, "  url      %s\n", url)
	emitf(out, "  user     %s\n", user)
	cacheLine := cachePath
	if cacheDefaulted {
		cacheLine += "  (default location)"
	}
	emitf(out, "  cache    %s\n", cacheLine)
	if caBundle != "" {
		emitf(out, "  ca       %s\n", caBundle)
	}
	emitf(out, "next: run `harmos sync` to populate the cache\n")
	return nil
}

func configPathOrDefault(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	return config.DefaultPath()
}

// confirmOverwrite decides whether to write a profile named name. proceed is
// false (with a nil error) when the user declined an overwrite; overwrite tells
// the config writer whether to replace an existing block.
func confirmOverwrite(cfgPath, name string, force bool, out io.Writer) (proceed, overwrite bool, err error) {
	exists, err := config.ProfileExists(cfgPath, name)
	if err != nil {
		return false, false, err
	}
	if !exists {
		return true, false, nil
	}
	if force {
		return true, true, nil
	}
	if !onTTY() {
		return false, false, fmt.Errorf("a profile named %q already exists in %s; pass --force to overwrite", name, cfgPath)
	}
	ok, err := confirm(fmt.Sprintf("profile %q already exists — overwrite? [y/N] ", name))
	if err != nil {
		return false, false, err
	}
	if !ok {
		emitf(out, "kept the existing %q; nothing changed\n", name)
		return false, false, nil
	}
	return true, true, nil
}

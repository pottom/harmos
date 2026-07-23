package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "List configured sources (no unlock needed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSources(configPath, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runSources(configPath string, out io.Writer) error {
	cfg, err := loadConfigAt(configPath)
	if err != nil {
		return err
	}
	for _, p := range cfg.Profiles {
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		emitf(out, "%s\t%s\t%s\n", p.Name, p.Type, loc)
	}
	return nil
}

func newAddKdbxCmd() *cobra.Command {
	var name, keyfile, configPath string
	var force, savePassword bool
	cmd := &cobra.Command{
		Use:   "add-kdbx <path>",
		Short: "Register a local .kdbx file as a read-only source",
		Long: "Add a local KeePass .kdbx file to the config as a read-only source. " +
			"harmos never writes to the file; you are prompted for its password when " +
			"you open it (or point at a key file with --keyfile). With --save-password " +
			"the password is stored in the OS keyring so you are not asked again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			derived := name
			if derived == "" {
				derived = deriveProfileName(args[0])
			}
			if err := runAddKdbx(configPath, args[0], name, keyfile, force, cmd.OutOrStdout()); err != nil {
				return err
			}
			if savePassword {
				return savePasswordFor(derived, cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "profile name (default: the file name without its extension)")
	cmd.Flags().StringVar(&keyfile, "keyfile", "", "key file, if the kdbx uses one")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing profile of the same name without asking")
	cmd.Flags().BoolVar(&savePassword, "save-password", false, "prompt for the password and store it in the OS keyring")
	cmd.Flags().StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	return cmd
}

func runAddKdbx(configPath, path, name, keyfile string, force bool, out io.Writer) error {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return err
		}
		configPath = p
	}

	kdbxPath, err := absPath(path)
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
		if keyfile, err = absPath(keyfile); err != nil {
			return err
		}
		if _, err := os.Stat(keyfile); err != nil {
			return fmt.Errorf("keyfile: %w", err)
		}
	}

	if name == "" {
		name = deriveProfileName(kdbxPath)
	}
	if name == "" {
		return fmt.Errorf("could not derive a profile name from %q; pass --name", path)
	}

	block := buildKdbxBlock(name, kdbxPath, keyfile)

	// No config yet: write a fresh file with just this source.
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(configPath, []byte(block)); err != nil {
			return err
		}
		return reportAdd(out, "added", name, kdbxPath)
	} else if statErr != nil {
		return statErr
	}

	// The file exists: Load validates it and tells us whether the name is taken.
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	verb := "added"
	var next string
	if cfg.Profile(name) != nil {
		if !force {
			if !onTTY() {
				return fmt.Errorf("a profile named %q already exists in %s; pass --force to overwrite", name, configPath)
			}
			ok, err := confirm(fmt.Sprintf("profile %q already exists — overwrite? [y/N] ", name))
			if err != nil {
				return err
			}
			if !ok {
				emitf(out, "kept the existing %q; nothing changed\n", name)
				return nil
			}
		}
		// Surgery: rewrite only this profile's block, leaving the rest verbatim.
		replaced, ok := replaceProfileBlock(string(content), name, strings.TrimRight(block, "\n"))
		if !ok {
			return fmt.Errorf("could not locate the %q profile block to overwrite in %s", name, configPath)
		}
		next, verb = replaced, "updated"
	} else {
		// Append the new block, leaving everything already there untouched.
		base := string(content)
		if !strings.HasSuffix(base, "\n") {
			base += "\n"
		}
		next = base + "\n" + block
	}

	if err := writeFileAtomic(configPath, []byte(next)); err != nil {
		return err
	}
	// A round-trip parse guards against writing a file that no longer loads.
	if _, err := config.Load(configPath); err != nil {
		return fmt.Errorf("config is invalid after %s %q: %w", verb, name, err)
	}
	return reportAdd(out, verb, name, kdbxPath)
}

func reportAdd(out io.Writer, verb, name, path string) error {
	emitf(out, "%s kdbx source %q → %s\n", verb, name, path)
	emitf(out, "run `harmos` to browse it (you'll be asked for its password)\n")
	return nil
}

func buildKdbxBlock(name, path, keyfile string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[profile]]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "type = %q\n", string(config.Kdbx))
	fmt.Fprintf(&b, "path = %q\n", path)
	if keyfile != "" {
		fmt.Fprintf(&b, "keyfile = %q\n", keyfile)
	}
	return b.String()
}

// replaceProfileBlock rewrites the [[profile]] block whose name matches, leaving
// every other line — comments, blank lines, other profiles, top-level keys —
// exactly as it was. A block is the header line plus the contiguous run of key
// lines under it, up to the first blank line or the next table header.
func replaceProfileBlock(content, name, newBlock string) (string, bool) {
	lines := strings.Split(content, "\n")
	isTableHeader := func(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "[") }

	for i := range lines {
		if strings.TrimSpace(lines[i]) != "[[profile]]" {
			continue
		}
		j := i + 1
		blockName := ""
		for j < len(lines) {
			t := strings.TrimSpace(lines[j])
			if t == "" || isTableHeader(lines[j]) {
				break
			}
			if k, v, ok := parseKV(t); ok && k == "name" {
				blockName = v
			}
			j++
		}
		if blockName != name {
			continue
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:i]...)
		out = append(out, strings.Split(newBlock, "\n")...)
		out = append(out, lines[j:]...)
		return strings.Join(out, "\n"), true
	}
	return content, false
}

// parseKV splits a `key = value` line, unquoting a string value. It ignores
// comment lines.
func parseKV(line string) (key, val string, ok bool) {
	if strings.HasPrefix(line, "#") {
		return "", "", false
	}
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(k)
	val = strings.TrimSpace(v)
	if len(val) >= 2 && val[0] == '"' {
		if uq, err := strconv.Unquote(val); err == nil {
			val = uq
		}
	} else if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		val = val[1 : len(val)-1]
	}
	return key, val, true
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".harmos-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// deriveProfileName is the default profile name for a kdbx path: the file name
// without its extension.
func deriveProfileName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// absPath expands a leading ~ and makes the path absolute.
func absPath(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		switch {
		case p == "~":
			p = home
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		}
	}
	return filepath.Abs(p)
}

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
	rows := make([][]string, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		loc := p.Path
		if p.Type == config.Pleasant {
			loc = p.URL
		}
		rows = append(rows, []string{p.Name, string(p.Type), loc})
	}
	printTable(out, []string{"NAME", "TYPE", "LOCATION"}, rows, showHeaders)
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
					derived = deriveProfileName(args[0])
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
	verb, err := upsertProfile(configPath, name, block, force, out)
	if err != nil || verb == "" {
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
	if url == "" || user == "" {
		return fmt.Errorf("a Pleasant source needs --url and --user")
	}
	cacheDefaulted := cache == ""
	if cacheDefaulted {
		if name == "" {
			return fmt.Errorf("a Pleasant source needs --name (or --cache) so its cache can be named")
		}
		p, err := defaultCachePath(name)
		if err != nil {
			return err
		}
		cache = p
	}
	cachePath, err := absPath(cache)
	if err != nil {
		return err
	}
	if caBundle != "" {
		if caBundle, err = absPath(caBundle); err != nil {
			return err
		}
		if _, err := os.Stat(caBundle); err != nil {
			return fmt.Errorf("ca-bundle: %w", err)
		}
	}
	if name == "" {
		name = deriveProfileName(cachePath)
	}
	if name == "" {
		return fmt.Errorf("could not derive a profile name; pass --name")
	}

	// Make sure the cache directory exists so `harmos sync` can write into it.
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	block := buildPleasantBlock(name, url, user, cachePath, caBundle)
	verb, err := upsertProfile(configPath, name, block, force, out)
	if err != nil || verb == "" {
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

// defaultCachePath is where a Pleasant source's cache lives when --cache is
// omitted: $XDG_DATA_HOME/harmos/<name>.kdbx, falling back to ~/.local/share.
func defaultCachePath(name string) (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "harmos", name+".kdbx"), nil
}

// upsertProfile inserts block into the config as a new profile, or — if a profile
// of that name exists — rewrites just that block (leaving the rest of the file
// verbatim), after confirming the overwrite. It returns "added" or "updated", or
// "" when an overwrite was declined (nothing changed).
func upsertProfile(configPath, name, block string, force bool, out io.Writer) (string, error) {
	if configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return "", err
		}
		configPath = p
	}

	// No config yet: write a fresh file with just this source.
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return "", err
		}
		if err := writeFileAtomic(configPath, []byte(block)); err != nil {
			return "", err
		}
		return "added", nil
	} else if statErr != nil {
		return "", statErr
	}

	// The file exists: Load validates it and tells us whether the name is taken.
	// A file that parses but has no profiles yet is fine — we append to it.
	cfg, err := config.Load(configPath)
	if err != nil {
		if !errors.Is(err, config.ErrNoProfiles) {
			return "", err
		}
		cfg = &config.Config{}
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}

	verb := "added"
	var next string
	if cfg.Profile(name) != nil {
		if !force {
			if !onTTY() {
				return "", fmt.Errorf("a profile named %q already exists in %s; pass --force to overwrite", name, configPath)
			}
			ok, err := confirm(fmt.Sprintf("profile %q already exists — overwrite? [y/N] ", name))
			if err != nil {
				return "", err
			}
			if !ok {
				emitf(out, "kept the existing %q; nothing changed\n", name)
				return "", nil
			}
		}
		// Surgery: rewrite only this profile's block, leaving the rest verbatim.
		replaced, ok := replaceProfileBlock(string(content), name, strings.TrimRight(block, "\n"))
		if !ok {
			return "", fmt.Errorf("could not locate the %q profile block to overwrite in %s", name, configPath)
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
		return "", err
	}
	// A round-trip parse guards against writing a file that no longer loads.
	if _, err := config.Load(configPath); err != nil {
		return "", fmt.Errorf("config is invalid after %s %q: %w", verb, name, err)
	}
	return verb, nil
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

func buildPleasantBlock(name, url, user, cache, caBundle string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[[profile]]\n")
	fmt.Fprintf(&b, "name = %q\n", name)
	fmt.Fprintf(&b, "type = %q\n", string(config.Pleasant))
	fmt.Fprintf(&b, "url = %q\n", url)
	fmt.Fprintf(&b, "user = %q\n", user)
	fmt.Fprintf(&b, "cache = %q\n", cache)
	if caBundle != "" {
		fmt.Fprintf(&b, "ca_bundle = %q\n", caBundle)
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

// removeProfileBlock deletes the [[profile]] block whose name matches (plus one
// trailing blank line so no double gap is left), leaving everything else — other
// profiles, comments, top-level keys — exactly as it was.
func removeProfileBlock(content, name string) (string, bool) {
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
		if j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++ // swallow the separating blank line
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:i]...)
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

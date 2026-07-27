package main

import (
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/pottom/harmos/internal/config"
	"github.com/pottom/harmos/internal/pwgen"
)

func newGenCmd() *cobra.Command {
	var (
		configPath                                            string
		length, count                                         int
		exclude                                               string
		noLower, noUpper, noDigit, noSymbol, noAmbig, oneEach bool
		doCopy                                                bool
	)
	cmd := &cobra.Command{
		Use:     "gen",
		Aliases: []string{"generate"},
		Short:   "Generate random password(s) with crypto/rand",
		Long: "Generate one or more random passwords using crypto/rand.\n\n" +
			"With no flags it uses your saved generator options (from the config,\n" +
			"same as the Generate tab); any flag overrides that option.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o, timeout := genDefaults(configPath)
			f := cmd.Flags()
			if f.Changed("length") {
				o.Length = length
			}
			if f.Changed("no-lower") {
				o.Lower = !noLower
			}
			if f.Changed("no-upper") {
				o.Upper = !noUpper
			}
			if f.Changed("no-digit") {
				o.Digit = !noDigit
			}
			if f.Changed("no-symbol") {
				o.Symbol = !noSymbol
			}
			if f.Changed("no-ambiguous") {
				o.AvoidAmbig = noAmbig
			}
			if f.Changed("one-each") {
				o.OneEach = oneEach
			}
			if f.Changed("exclude") {
				o.Exclude = exclude
			}
			return runGen(o, count, doCopy, timeout, cmd.OutOrStdout())
		},
	}
	f := cmd.Flags()
	f.StringVar(&configPath, "config", "", "config file (default: $XDG_CONFIG_HOME/harmos/config.toml)")
	f.IntVarP(&length, "length", "n", 20, "password length (8–64)")
	f.IntVarP(&count, "count", "c", 1, "how many to generate")
	f.StringVarP(&exclude, "exclude", "x", "", "characters to keep out of the pool")
	f.BoolVarP(&noLower, "no-lower", "L", false, "exclude a–z")
	f.BoolVarP(&noUpper, "no-upper", "U", false, "exclude A–Z")
	f.BoolVarP(&noDigit, "no-digit", "D", false, "exclude 0–9")
	f.BoolVarP(&noSymbol, "no-symbol", "S", false, "exclude symbols")
	f.BoolVarP(&noAmbig, "no-ambiguous", "a", false, "drop ambiguous glyphs (0 O 1 l I)")
	f.BoolVarP(&oneEach, "one-each", "e", false, "require at least one of each enabled class")
	f.BoolVarP(&doCopy, "copy", "y", false, "copy to the clipboard (concealed, auto-cleared) instead of printing")
	return cmd
}

func runGen(o pwgen.Options, count int, doCopy bool, timeout time.Duration, out io.Writer) error {
	if count < 1 {
		count = 1
	}
	ps, err := pwgen.Many(o, count)
	if err != nil {
		return err
	}
	if doCopy {
		// Copy the first (concealed, auto-clearing); print the rest, if any.
		if err := copyConcealed([]byte(ps[0]), "generated password", false, timeout); err != nil {
			return err
		}
		return nil
	}
	for _, p := range ps {
		emitf(out, "%s\n", p)
	}
	return nil
}

// genDefaults returns the generator options and clipboard timeout from the config
// (falling back to built-in defaults), so `harmos gen` matches the Generate tab.
func genDefaults(configPath string) (pwgen.Options, time.Duration) {
	o := pwgen.Default()
	timeout := config.DefaultClipboardTimeout

	path := configPath
	if path == "" {
		if p, err := config.DefaultPath(); err == nil {
			path = p
		}
	}
	if path == "" {
		return o, timeout
	}
	cfg, err := config.Load(path)
	if err != nil {
		return o, timeout // no (usable) config — built-in defaults
	}
	if cfg.ClipboardTimeout.Duration > 0 {
		timeout = cfg.ClipboardTimeout.Duration
	}
	if cfg.GenLength >= pwgen.MinLength && cfg.GenLength <= pwgen.MaxLength {
		o.Length = cfg.GenLength
	}
	setBoolFlag(&o.Lower, cfg.GenLower)
	setBoolFlag(&o.Upper, cfg.GenUpper)
	setBoolFlag(&o.Digit, cfg.GenDigit)
	setBoolFlag(&o.Symbol, cfg.GenSymbol)
	setBoolFlag(&o.AvoidAmbig, cfg.GenNoAmbig)
	setBoolFlag(&o.OneEach, cfg.GenOneEach)
	return o, timeout
}

func setBoolFlag(dst, src *bool) {
	if src != nil {
		*dst = *src
	}
}

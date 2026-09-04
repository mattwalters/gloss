package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
)

func runSettings(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"settings"}, settingsCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"settings"}, settingsCmd)
		return 0
	case "get":
		return runSettingsGet(ctx, defaultDir, args[1:], stdout, stderr)
	case "set":
		return runSettingsSet(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ settings: unknown command %q\n\n", args[0])
		renderUsage(stderr, []string{"settings"}, settingsCmd)
		return 2
	}
}

type settingsGetOpts struct {
	dir      string
	jsonMode bool
}

func newSettingsGetFlagSet(defaultDir string) (*flag.FlagSet, *settingsGetOpts) {
	fs := flag.NewFlagSet("settings get", flag.ContinueOnError)
	opts := &settingsGetOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	return fs, opts
}

func runSettingsGet(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newSettingsGetFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ settings get: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	res, err := store.Query.Settings()
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		wireSettings := wire.FromSettingsResult(res)
		if err := emitJSON(stdout, wire.KindSettings, wireSettings); err != nil {
			fmt.Fprintf(stderr, "writ settings get: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "Name:\t%s\n", res.Settings.Name)
	fmt.Fprintf(tw, "Identifier:\t%s\n", res.Settings.Identifier)
	fmt.Fprintf(tw, "Timezone:\t%s\n", res.Settings.Timezone)
	fmt.Fprintf(tw, "Estimate Scale:\t%s\n", res.Settings.EstimateScale)
	fmt.Fprintf(tw, "Allow Zero Estimates:\t%t\n", res.Settings.AllowZeroEstimates)
	fmt.Fprintf(tw, "Cycles Enabled:\t%t\n", res.Settings.CyclesEnabled)
	fmt.Fprintf(tw, "Cycle Duration (weeks):\t%d\n", res.Settings.CycleDurationWeeks)
	fmt.Fprintf(tw, "Cycle Start Day:\t%d\n", res.Settings.CycleStartDay)
	fmt.Fprintf(tw, "Cycle Cooldown (weeks):\t%d\n", res.Settings.CycleCooldownWeeks)
	fmt.Fprintf(tw, "Triage Enabled:\t%t\n", res.Settings.TriageEnabled)

	if len(res.Settings.UnknownKeys) > 0 {
		var keys []string
		for k := range res.Settings.UnknownKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(tw, "%s:\t%v\n", k, res.Settings.UnknownKeys[k])
		}
	}
	tw.Flush()
	return 0
}

type settingsSetOpts struct {
	dir                string
	jsonMode           bool
	name               string
	identifier         string
	timezone           string
	estimateScale      string
	allowZeroEstimates string
	cyclesEnabled      string
	cycleDuration      int
	cycleStartDay      int
	cycleCooldown      int
	triageEnabled      string
}

func newSettingsSetFlagSet(defaultDir string) (*flag.FlagSet, *settingsSetOpts) {
	fs := flag.NewFlagSet("settings set", flag.ContinueOnError)
	opts := &settingsSetOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.StringVar(&opts.name, "name", "", "Workspace display name")
	fs.StringVar(&opts.identifier, "identifier", "", "Issue identifier prefix")
	fs.StringVar(&opts.timezone, "timezone", "", "IANA timezone identifier")
	fs.StringVar(&opts.estimateScale, "estimate-scale", "", "Estimate scale (none, fibonacci, exponential, linear, t-shirt)")
	fs.StringVar(&opts.allowZeroEstimates, "allow-zero-estimates", "", "Allow zero as estimate (true|false)")
	fs.StringVar(&opts.cyclesEnabled, "cycles-enabled", "", "Enable cycles (true|false)")
	fs.IntVar(&opts.cycleDuration, "cycle-duration", 0, "Cycle duration in weeks")
	fs.IntVar(&opts.cycleStartDay, "cycle-start-day", 0, "Cycle start day (1=Monday, 7=Sunday)")
	fs.IntVar(&opts.cycleCooldown, "cycle-cooldown", 0, "Cycle cooldown in weeks")
	fs.StringVar(&opts.triageEnabled, "triage-enabled", "", "Enable triage mode (true|false)")
	return fs, opts
}

func runSettingsSet(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newSettingsSetFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ settings set: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	var edit writ.SettingsEdit
	var hasField bool
	var visitErr error

	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "name":
			edit.Name = &opts.name
			hasField = true
		case "identifier":
			edit.Identifier = &opts.identifier
			hasField = true
		case "timezone":
			edit.Timezone = &opts.timezone
			hasField = true
		case "estimate-scale":
			edit.EstimateScale = &opts.estimateScale
			hasField = true
		case "allow-zero-estimates":
			b, err := strconv.ParseBool(opts.allowZeroEstimates)
			if err != nil {
				visitErr = fmt.Errorf("writ settings set: invalid boolean for --allow-zero-estimates %q: expected true or false", opts.allowZeroEstimates)
				return
			}
			edit.AllowZeroEstimates = &b
			hasField = true
		case "cycles-enabled":
			b, err := strconv.ParseBool(opts.cyclesEnabled)
			if err != nil {
				visitErr = fmt.Errorf("writ settings set: invalid boolean for --cycles-enabled %q: expected true or false", opts.cyclesEnabled)
				return
			}
			edit.CyclesEnabled = &b
			hasField = true
		case "cycle-duration":
			edit.CycleDurationWeeks = &opts.cycleDuration
			hasField = true
		case "cycle-start-day":
			edit.CycleStartDay = &opts.cycleStartDay
			hasField = true
		case "cycle-cooldown":
			edit.CycleCooldownWeeks = &opts.cycleCooldown
			hasField = true
		case "triage-enabled":
			b, err := strconv.ParseBool(opts.triageEnabled)
			if err != nil {
				visitErr = fmt.Errorf("writ settings set: invalid boolean for --triage-enabled %q: expected true or false", opts.triageEnabled)
				return
			}
			edit.TriageEnabled = &b
			hasField = true
		}
	})

	if visitErr != nil {
		fmt.Fprintln(stderr, visitErr.Error())
		return 2
	}

	if !hasField {
		fmt.Fprintln(stderr, "writ settings set: at least one setting flag is required")
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	if err := store.Settings.Set(ctx, edit); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		res, err := store.Query.Settings()
		if err != nil {
			return renderErr(stderr, err)
		}
		wireSettings := wire.FromSettingsResult(res)
		if err := emitJSON(stdout, wire.KindSettings, wireSettings); err != nil {
			fmt.Fprintf(stderr, "writ settings set: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintln(stdout, "Updated workspace settings.")
	return 0
}

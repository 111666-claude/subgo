package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kianelbo/subgo"
)

var (
	outputFile  string
	shiftAmount time.Duration
	stretch     float64
	clamp       bool
	trimFirst   int
	trimLast    int
	trimBefore  time.Duration
	trimAfter   time.Duration
	removeHI    bool
	removeIds   []uint
	sliceRange  string
	unsafeTags  string
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "subgo <input-file>",
	Short: "A tool for working with subtitle files",
	Long: `subgo is a CLI tool for processing subtitle files.
It can perform various operations on subtitle files and conversion between formats.

Examples:
  subgo input.srt --shift 300ms --output output.srt
  subgo input.srt --shift -1s --stretch 1.05
  subgo input.ass --output output.srt
  subgo input.srt --trim-before 1m --trim-after 1h49m30s
  subgo input.srt --trim-first 5 --trim-last 2
  subgo input.ass --slice 1m30s-2m --output clip.ass`,
	Args: cobra.ExactArgs(1),
	RunE: run,
}

func init() {
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "output.srt", "output subtitle file")
	rootCmd.Flags().DurationVarP(&shiftAmount, "shift", "s", 0, "shift amount (e.g., 300ms, 2s, -1s500ms)")
	rootCmd.Flags().Float64VarP(&stretch, "stretch", "t", 1.0, "stretch factor (e.g., 1.05 for 5% longer)")
	rootCmd.Flags().BoolVarP(&clamp, "clamp", "c", true, "clamp negative times to zero")
	rootCmd.Flags().IntVar(&trimFirst, "trim-first", 0, "remove first n events")
	rootCmd.Flags().IntVar(&trimLast, "trim-last", 0, "remove last n events")
	rootCmd.Flags().DurationVar(&trimBefore, "trim-before", 0, "remove events before timestamp (e.g., 1m, 1h30m)")
	rootCmd.Flags().DurationVar(&trimAfter, "trim-after", 0, "remove events after timestamp (e.g., 1h49m30s)")
	rootCmd.Flags().BoolVar(&removeHI, "remove-hi", false, "remove hearing impaired annotations like (sobbing) and speaker labels")
	rootCmd.Flags().UintSliceVar(&removeIds, "remove-ids", make([]uint, 0), "remove events by comma-separated indices")
	rootCmd.Flags().StringVar(&sliceRange, "slice", "", "export a clip as START-END (e.g., 1m30s-2m)")
	rootCmd.Flags().StringVar(&unsafeTags, "unsafe-tags", "preserve", "how to handle untransformable ASS animation tags: preserve or error")
}

// parseSliceRange parses the START-END value accepted by --slice.
func parseSliceRange(value string) (time.Duration, time.Duration, error) {
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid slice range %q: expected START-END", value)
	}
	start, err := time.ParseDuration(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slice start %q: %w", parts[0], err)
	}
	end, err := time.ParseDuration(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slice end %q: %w", parts[1], err)
	}
	return start, end, nil
}

func run(cmd *cobra.Command, args []string) error {
	inputFile := args[0]

	// Load subtitle file
	sub, err := subgo.Load(inputFile)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}

	// Apply operations
	sub = applyOperations(sub)

	if cmd.Flags().Changed("slice") {
		start, end, err := parseSliceRange(sliceRange)
		if err != nil {
			return err
		}
		policy := subgo.UnsafeTagPreserve
		switch strings.ToLower(unsafeTags) {
		case "preserve":
		case "error":
			policy = subgo.UnsafeTagError
		default:
			return fmt.Errorf("invalid --unsafe-tags %q: expected preserve or error", unsafeTags)
		}
		result, err := sub.SliceWithOptions(start, end, subgo.SliceOptions{UnsafeTagPolicy: policy})
		if err != nil {
			return err
		}
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintf(os.Stderr, "slice: %s\n", describeDiagnostic(diagnostic))
		}
		sub = result.Subtitle
	}

	// Save subtitle file
	if err := sub.Save(outputFile); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	return nil
}

func describeDiagnostic(d subgo.Diagnostic) string {
	location := "clip"
	if d.EventIndex >= 0 {
		location = fmt.Sprintf("event %d", d.EventIndex+1)
		if d.Offset >= 0 {
			location += fmt.Sprintf(" offset %d", d.Offset)
		}
	}
	return fmt.Sprintf("%s (%s): %s", location, d.Code, d.Message)
}

func applyOperations(sub subgo.Subtitle) subgo.Subtitle {
	// Apply the removes first
	if len(removeIds) > 0 {
		sub = sub.RemoveIds(removeIds)
	}

	// Apply trimming first (before other operations)
	if trimFirst > 0 {
		sub = sub.TrimFirst(trimFirst)
	}
	if trimLast > 0 {
		sub = sub.TrimLast(trimLast)
	}
	if trimBefore > 0 {
		sub = sub.TrimBefore(trimBefore)
	}
	if trimAfter > 0 {
		sub = sub.TrimAfter(trimAfter)
	}

	// Apply shift if specified
	if shiftAmount != 0 {
		sub = sub.Shift(shiftAmount, clamp)
	}

	// Apply stretch if not 1.0
	if stretch != 1.0 {
		sub = sub.Stretch(stretch, 0)
	}

	// Remove HI annotations if requested
	if removeHI {
		sub = sub.RemoveHI()
	}

	return sub
}

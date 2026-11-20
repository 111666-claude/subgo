[![Go Reference](https://pkg.go.dev/badge/github.com/kianelbo/subgo.svg)](https://pkg.go.dev/github.com/kianelbo/subgo)
[![codecov](https://codecov.io/gh/kianelbo/subgo/branch/main/graph/badge.svg)](https://codecov.io/gh/kianelbo/subgo)

Subgo is a modern, lightweight Go library and command-line tool for manipulating subtitle files. It provides a simple, consistent API for loading, transforming, and exporting subtitles across multiple formats.

## Installation

For your go projects:
```bash
go get github.com/kianelbo/subgo@latest
```

For the CLI, standalone binaries are available [here](https://github.com/kianelbo/subgo/releases).

## CLI Usage

```bash
subgo <input-file> [flags]
```

### Features

| Flag | Description |
|------|-------------|
| `-o, --output` | Output file (default: output.srt) |
| `-s, --shift` | Shift timing (e.g., `300ms`, `-1s`) |
| `-t, --stretch` | Stretch factor (e.g., `1.05` for 5% longer) |
| `-c, --clamp` | Clamp negative times to zero (default: true) |
| `--trim-first` | Remove first n events |
| `--trim-last` | Remove last n events |
| `--trim-before` | Remove events before timestamp |
| `--trim-after` | Remove events after timestamp |
| `--remove-hi` | Remove hearing impaired annotations |
| `--remove-ids` | Remove events by comma-separated indices |
| `--slice` | Export a clip as `START-END` (e.g., `1m30s-2m`); times are rebased to zero |
| `--unsafe-tags` | ASS animation tags that cannot be retimed: `preserve` (default) or `error` |

#### Example

```bash
subgo eraserhead.srt --trim-before 1m --shift 300ms --remove-hi -o eraserhead.srt
subgo opening.ass --slice 1m30s-2m -o opening-clip.ass
```

#### Slicing a clip

`--slice START-END` keeps every event that overlaps the range. Events crossing a
boundary are clipped to it, and the clip is rebased so it starts at zero. ASS/SSA
script info, styles, attached fonts and the original dialogue fields are kept.

Time-bearing ASS override tags are adjusted for a clipped cue head:

| Tag | Behaviour |
|-----|-----------|
| `\move(x1,y1,x2,y2)` | Converted to an explicit move that continues from the position reached at the clip start. |
| `\move(x1,y1,x2,y2,t1,t2)` | `t1`/`t2` are shifted by the trimmed amount. |
| `\fade(a1,a2,a3,t1,t2,t3,t4)` | `t1`–`t4` are shifted by the trimmed amount. |
| `\t(t1,t2,...)`, `\kt(t)` | Times are shifted by the trimmed amount. |

Tags that cannot be retimed safely (`\k`, `\kf`, `\ko`, or timed tags whose
start would land before the clip) are reported as `unsafe-tag` diagnostics with
the event index and text offset, and are left untouched by default. Pass
`--unsafe-tags error` to fail instead. `\fad` is preserved as-is because it is
relative to the cue; a `\fad` longer than the clipped cue is reported as
`out-of-range-tag`.

Diagnostics are printed to stderr with a code and location, for example:

```text
slice: event 3 offset 1 (unsafe-tag): karaoke timings cannot be retimed after clipping the cue head
```

Other codes are `empty-slice` (the range contains no events), `missing-style`
(an event references an undeclared style) and `duplicate-event` (two output
events have identical timing, text and ASS fields). A reversed range or a
negative start is a fatal error.

## Library Usage

```go
import "github.com/kianelbo/subgo"

sub, _ := subgo.Load("input.ass")
sub = sub.Shift(300*time.Millisecond, true)
sub = sub.Stretch(1.05, 0)
sub.Save("output.srt")

// Export the 90s-120s clip, keeping ASS styles and script info.
clip, err := sub.Slice(90*time.Second, 2*time.Minute)
if err != nil {
    return err
}
for _, d := range clip.Diagnostics {
    log.Printf("event %d: %s: %s", d.EventIndex+1, d.Code, d.Message)
}
// Make untransformable animation tags fatal instead of diagnostic:
// clip, err = sub.SliceWithOptions(start, end, subgo.SliceOptions{UnsafeTagPolicy: subgo.UnsafeTagError})
clip.Subtitle.Save("clip.ass")
```

## Supported Formats
- SRT
- ASS/SSA
- WebVTT
- SBV
- TTML

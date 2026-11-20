package subgo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSliceSRT(t *testing.T) {
	sub, err := Load(filepath.Join("testdata", "fixtures", "plain.srt"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	result, err := sub.Slice(2*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %+v", result.Diagnostics)
	}

	want := []Event{
		{Start: 0, End: 2 * time.Second, Text: "Crossing the slice head"},
		{Start: 2500 * time.Millisecond, End: 3500 * time.Millisecond, Text: "Inside the clip"},
		{Start: 7 * time.Second, End: 8 * time.Second, Text: "Crossing the slice tail"},
	}
	if len(result.Subtitle.Events) != len(want) {
		t.Fatalf("got %d events, want %d", len(result.Subtitle.Events), len(want))
	}
	for i, e := range result.Subtitle.Events {
		if e.Start != want[i].Start || e.End != want[i].End || e.Text != want[i].Text {
			t.Errorf("event %d = %+v, want %+v", i, e, want[i])
		}
	}

	// The clip must round-trip through SRT unchanged.
	var buf bytes.Buffer
	if err := (srtFormat{}).Encode(&buf, result.Subtitle); err != nil {
		t.Fatalf("encode: %v", err)
	}
	redecoded, err := (srtFormat{}).Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(redecoded.Events) != len(want) {
		t.Fatalf("round trip got %d events, want %d", len(redecoded.Events), len(want))
	}
	for i, e := range redecoded.Events {
		if e.Start != want[i].Start || e.End != want[i].End || e.Text != want[i].Text {
			t.Errorf("round trip event %d = %+v, want %+v", i, e, want[i])
		}
	}
}

func TestSliceASSPreservesScriptAndStyles(t *testing.T) {
	sub, err := Load(filepath.Join("testdata", "fixtures", "cross_clip.ass"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	result, err := sub.Slice(5*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}

	if len(result.Subtitle.Events) != 4 {
		t.Fatalf("got %d events, want 4", len(result.Subtitle.Events))
	}
	if result.Subtitle.ASS == nil {
		t.Fatal("ASS metadata was dropped")
	}
	if !result.Subtitle.ASS.hasSection("Script Info") {
		t.Error("script info was dropped")
	}

	// The style table is carried verbatim.
	var buf bytes.Buffer
	if err := (assFormat{}).Encode(&buf, result.Subtitle); err != nil {
		t.Fatalf("encode: %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		"Title: Cross-clip animation fixture",
		"PlayResX: 1920",
		"Style: Title,Arial,72",
		"Style: Caption,Arial,36",
		"Dialogue: 0,0:00:00.00,0:00:05.00,Title,,0,0,0,,{\\move(500,100,900,100,0,5000)}Fly in",
		"Dialogue: 0,0:00:00.00,0:00:01.00,Caption,,0,0,0,,{\\fad(200,200)}Hello there",
		"Dialogue: 0,0:00:00.00,0:00:05.00,Caption,,0,0,0,,{\\fade(0,0,255,0,500,1000,2000)}Fading in and out",
		"Dialogue: 0,0:00:04.00,0:00:05.00,Caption,,0,0,0,,{\\t(0,500,\\frz10)}Rotate",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Missing style") || strings.Contains(output, "Outside the clip") {
		t.Errorf("output contains events outside the clip:\n%s", output)
	}

	// The clip must decode back to the same events.
	decoded, err := (assFormat{}).Decode(strings.NewReader(output))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Events) != len(result.Subtitle.Events) {
		t.Fatalf("redecoded %d events, want %d", len(decoded.Events), len(result.Subtitle.Events))
	}
	for i, want := range result.Subtitle.Events {
		got := decoded.Events[i]
		if abs(got.Start-want.Start) > 10*time.Millisecond || abs(got.End-want.End) > 10*time.Millisecond {
			t.Errorf("event %d timing = %v-%v, want %v-%v", i, got.Start, got.End, want.Start, want.End)
		}
		if got.Text != want.Text {
			t.Errorf("event %d text = %q, want %q", i, got.Text, want.Text)
		}
		style, ok := got.ASS.Value("style")
		if !ok || style != mustStyle(t, want.ASS) {
			t.Errorf("event %d style = %q, want %q", i, style, mustStyle(t, want.ASS))
		}
	}
}

func mustStyle(t *testing.T, event *ASSEvent) string {
	t.Helper()
	if event == nil {
		return ""
	}
	return event.Style()
}

func TestSliceASSStyleAndNameFieldsPreserved(t *testing.T) {
	sub := Subtitle{
		ASS: &ASSMetadata{
			EventFormat: "Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text",
			Styles:      []string{"Default"},
			Sections: []ASSSection{
				{Name: "V4+ Styles", Lines: []string{
					"Format: Name, Fontname, Fontsize",
					"Style: Default,Arial,20",
				}},
			},
		},
		Events: []Event{{
			Start: time.Second,
			End:   2 * time.Second,
			Text:  "Hello",
			ASS: &ASSEvent{Fields: []ASSField{
				{Name: "layer", Value: "3"},
				{Name: "start", Value: "0:00:01.00"},
				{Name: "end", Value: "0:00:02.00"},
				{Name: "style", Value: "Default"},
				{Name: "name", Value: "Alice"},
				{Name: "marginl", Value: "10"},
				{Name: "marginr", Value: "20"},
				{Name: "marginv", Value: "30"},
				{Name: "effect", Value: "scroll up"},
				{Name: "text", Value: "Hello"},
			}},
		}},
	}

	result, err := sub.Slice(0, 3*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	var buf bytes.Buffer
	if err := (assFormat{}).Encode(&buf, result.Subtitle); err != nil {
		t.Fatalf("encode: %v", err)
	}
	want := "Dialogue: 3,0:00:01.00,0:00:02.00,Default,Alice,10,20,30,scroll up,Hello"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("output missing %q:\n%s", want, buf.String())
	}
}

func TestSliceEmptyRange(t *testing.T) {
	sub := Subtitle{Events: []Event{{Start: time.Second, End: 2 * time.Second, Text: "one"}}}

	result, err := sub.Slice(10*time.Second, 20*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if len(result.Subtitle.Events) != 0 {
		t.Errorf("got %d events, want 0", len(result.Subtitle.Events))
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != CodeEmptySlice {
		t.Errorf("diagnostics = %+v, want one %s", result.Diagnostics, CodeEmptySlice)
	}
	if result.Diagnostics[0].EventIndex != -1 {
		t.Errorf("empty slice diagnostic event index = %d, want -1", result.Diagnostics[0].EventIndex)
	}
}

func TestSliceRangeErrors(t *testing.T) {
	sub := Subtitle{Events: []Event{{Start: time.Second, End: 2 * time.Second, Text: "one"}}}

	tests := []struct {
		name  string
		start time.Duration
		end   time.Duration
		code  string
	}{
		{"reversed", 5 * time.Second, 2 * time.Second, CodeReversedRange},
		{"zero length", 2 * time.Second, 2 * time.Second, CodeEmptySlice},
		{"negative start", -time.Second, 2 * time.Second, CodeNegativeRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sub.Slice(tt.start, tt.end)
			if tt.code == CodeEmptySlice {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tt.code {
					t.Fatalf("diagnostics = %+v, want %s", result.Diagnostics, tt.code)
				}
				return
			}

			var sliceErr *SliceError
			if !errors.As(err, &sliceErr) {
				t.Fatalf("error = %v, want *SliceError", err)
			}
			if sliceErr.Code != tt.code {
				t.Errorf("code = %q, want %q", sliceErr.Code, tt.code)
			}
			if sliceErr.EventIndex != -1 || sliceErr.Offset != -1 {
				t.Errorf("location = event %d offset %d, want -1/-1", sliceErr.EventIndex, sliceErr.Offset)
			}
		})
	}
}

func TestSliceDiagnosticsAreLocatable(t *testing.T) {
	sub, err := Load(filepath.Join("testdata", "fixtures", "cross_clip.ass"))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	result, err := sub.Slice(10*time.Second, 20*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}

	// Event 5 starts exactly at the slice start and uses an undeclared style.
	var found bool
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != CodeMissingStyle {
			continue
		}
		found = true
		if diagnostic.EventIndex != 5 {
			t.Errorf("missing style event index = %d, want 5", diagnostic.EventIndex)
		}
		if diagnostic.Style != "Ghost" {
			t.Errorf("missing style = %q, want Ghost", diagnostic.Style)
		}
	}
	if !found {
		t.Errorf("missing %s diagnostic: %+v", CodeMissingStyle, result.Diagnostics)
	}
}

func TestSliceDuplicateEvents(t *testing.T) {
	event := Event{Start: 5 * time.Second, End: 6 * time.Second, Text: "same"}
	sub := Subtitle{Events: []Event{event, event}}

	result, err := sub.Slice(0, 10*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if len(result.Subtitle.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(result.Subtitle.Events))
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != CodeDuplicateEvent {
		t.Fatalf("diagnostics = %+v, want one %s", result.Diagnostics, CodeDuplicateEvent)
	}
	if result.Diagnostics[0].EventIndex != 1 {
		t.Errorf("duplicate event index = %d, want 1", result.Diagnostics[0].EventIndex)
	}
}

func TestSliceReportsOutOfRangeTag(t *testing.T) {
	sub := Subtitle{
		Events: []Event{{
			Start: 0,
			End:   10 * time.Second,
			Text:  `{\t(2000,9000,\frz10)}Rotate`,
		}},
	}

	result, err := sub.Slice(2*time.Second, 4*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if len(result.Subtitle.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(result.Subtitle.Events))
	}
	if got, want := result.Subtitle.Events[0].Text, `{\t(0,7000,\frz10)}Rotate`; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}

	var found bool
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code != CodeOutOfRangeTag {
			continue
		}
		found = true
		if diagnostic.EventIndex != 0 || diagnostic.Offset != 1 {
			t.Errorf("location = event %d offset %d, want 0/1", diagnostic.EventIndex, diagnostic.Offset)
		}
	}
	if !found {
		t.Errorf("missing %s diagnostic: %+v", CodeOutOfRangeTag, result.Diagnostics)
	}
}

func TestSliceUnsafeTagPolicy(t *testing.T) {
	sub := Subtitle{
		Events: []Event{{
			Start: 0,
			End:   10 * time.Second,
			Text:  `{\k50}Ka`,
		}},
	}

	t.Run("preserve keeps the tag", func(t *testing.T) {
		result, err := sub.Slice(5*time.Second, 8*time.Second)
		if err != nil {
			t.Fatalf("slice: %v", err)
		}
		if got, want := result.Subtitle.Events[0].Text, `{\k50}Ka`; got != want {
			t.Errorf("text = %q, want %q", got, want)
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != CodeUnsafeTag {
			t.Errorf("diagnostics = %+v, want one %s", result.Diagnostics, CodeUnsafeTag)
		}
	})

	t.Run("error aborts", func(t *testing.T) {
		_, err := sub.SliceWithOptions(5*time.Second, 8*time.Second, SliceOptions{UnsafeTagPolicy: UnsafeTagError})
		var sliceErr *SliceError
		if !errors.As(err, &sliceErr) {
			t.Fatalf("error = %v, want *SliceError", err)
		}
		if sliceErr.Code != CodeUnsafeTag || sliceErr.EventIndex != 0 || sliceErr.Offset != 1 {
			t.Errorf("error = %+v, want unsafe-tag at event 0 offset 1", sliceErr)
		}
		if !strings.Contains(sliceErr.Error(), "event 1 offset 1") || !strings.Contains(sliceErr.Error(), CodeUnsafeTag) {
			t.Errorf("error text %q is not locatable", sliceErr.Error())
		}
	})
}

func TestSliceRetimesASSTags(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		start time.Duration
		end   time.Duration
		want  string
	}{
		{
			name:  "fade times shift with a clipped head",
			text:  `{\fade(0,0,255,2000,2500,3000,3500)}x`,
			start: 1 * time.Second,
			end:   4 * time.Second,
			want:  `{\fade(0,0,255,1000,1500,2000,2500)}x`,
		},
		{
			name:  "explicit move times shift",
			text:  `{\move(0,0,100,100,2000,4000)}x`,
			start: 1 * time.Second,
			end:   3 * time.Second,
			want:  `{\move(0,0,100,100,1000,3000)}x`,
		},
		{
			name:  "implicit move continues from its clipped position",
			text:  `{\move(0,0,1000,0)}x`,
			start: 2 * time.Second,
			end:   5 * time.Second,
			want:  `{\move(400,0,1000,0,0,3000)}x`,
		},
		{
			name:  "tail clip leaves implicit move alone",
			text:  `{\move(0,0,1000,0)}x`,
			start: 0,
			end:   5 * time.Second,
			want:  `{\move(0,0,1000,0)}x`,
		},
		{
			name:  "nested transform arguments survive",
			text:  `{\b1\t(2000,3000,\frz10\bord0)}x`,
			start: 1 * time.Second,
			end:   3 * time.Second,
			want:  `{\b1\t(1000,2000,\frz10\bord0)}x`,
		},
		{
			name:  "untimed tags are untouched",
			text:  `{\an8\pos(100,200)\c&HFFFFFF&}x`,
			start: 1 * time.Second,
			end:   3 * time.Second,
			want:  `{\an8\pos(100,200)\c&HFFFFFF&}x`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := Subtitle{Events: []Event{{Start: 0, End: 5 * time.Second, Text: tt.text}}}
			result, err := sub.Slice(tt.start, tt.end)
			if err != nil {
				t.Fatalf("slice: %v", err)
			}
			if len(result.Subtitle.Events) != 1 {
				t.Fatalf("got %d events, want 1", len(result.Subtitle.Events))
			}
			if got := result.Subtitle.Events[0].Text; got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSliceDoesNotMutateSource(t *testing.T) {
	sub := Subtitle{
		ASS: &ASSMetadata{
			Styles:   []string{"Default"},
			Sections: []ASSSection{{Name: "V4+ Styles", Lines: []string{"Style: Default,Arial,20"}}},
		},
		Events: []Event{{
			Start: 0,
			End:   10 * time.Second,
			Text:  `{\fade(0,0,255,2000,2500,3000,3500)}x`,
			ASS: &ASSEvent{Fields: []ASSField{
				{Name: "style", Value: "Default"},
				{Name: "text", Value: `{\fade(0,0,255,2000,2500,3000,3500)}x`},
			}},
		}},
	}

	result, err := sub.Slice(time.Second, 3*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if got := sub.Events[0].Text; got != `{\fade(0,0,255,2000,2500,3000,3500)}x` {
		t.Errorf("source text mutated: %q", got)
	}
	if got := sub.Events[0].ASS.Fields[1].Value; got != `{\fade(0,0,255,2000,2500,3000,3500)}x` {
		t.Errorf("source ASS field mutated: %q", got)
	}
	if got := result.Subtitle.Events[0].ASS.Fields[1].Value; got != `{\fade(0,0,255,1000,1500,2000,2500)}x` {
		t.Errorf("clipped ASS field = %q, want the retimed text", got)
	}
}

func TestSliceOnDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join("testdata", "fixtures", "cross_clip.ass")

	sub, err := Load(input)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result, err := sub.Slice(5*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}

	output := filepath.Join(dir, "clip.ass")
	if err := result.Subtitle.Save(output); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(output)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Events) != len(result.Subtitle.Events) {
		t.Fatalf("reloaded %d events, want %d", len(reloaded.Events), len(result.Subtitle.Events))
	}
	for i, want := range result.Subtitle.Events {
		got := reloaded.Events[i]
		if abs(got.Start-want.Start) > 10*time.Millisecond || abs(got.End-want.End) > 10*time.Millisecond {
			t.Errorf("event %d timing = %v-%v, want %v-%v", i, got.Start, got.End, want.Start, want.End)
		}
		if got.Text != want.Text {
			t.Errorf("event %d text = %q, want %q", i, got.Text, want.Text)
		}
	}
	if reloaded.ASS == nil || !reloaded.ASS.hasSection("Script Info") {
		t.Error("script info lost on disk round trip")
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(raw), "Style: Caption,Arial,36") {
		t.Error("style table lost on disk round trip")
	}
}

func TestSliceLeavesEscapedBracesAlone(t *testing.T) {
	sub := Subtitle{Events: []Event{{
		Start: 0,
		End:   5 * time.Second,
		Text:  `a \{not a tag\} {\frz10}b`,
	}}}

	result, err := sub.Slice(time.Second, 3*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if got, want := result.Subtitle.Events[0].Text, `a \{not a tag\} {\frz10}b`; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestSliceClipConvertsToPlainFormat(t *testing.T) {
	// Slice metadata must survive a conversion to a format without ASS support.
	sub, err := Load(filepath.Join("testdata", "fixtures", "cross_clip.ass"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	result, err := sub.Slice(5*time.Second, 10*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	result.Subtitle = result.Subtitle.Shift(time.Second, true)

	var buf bytes.Buffer
	if err := (srtFormat{}).Encode(&buf, result.Subtitle); err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The fade-in cue is clipped to 0-1s, then shifted by one second.
	if !strings.Contains(buf.String(), "00:00:01,000 --> 00:00:02,000\n{\\fad(200,200)}Hello there") {
		t.Errorf("converted clip lost timing or text:\n%s", buf.String())
	}
}

func TestSliceKeepsTransformRelativeArguments(t *testing.T) {
	// Tag arguments nested inside \t are relative to the transform window, not
	// the cue, so only the \t times themselves move.
	sub := Subtitle{Events: []Event{{
		Start: 0,
		End:   5 * time.Second,
		Text:  `{\t(2000,3000,\fade(0,0,255,0,2500,3000,3500))}x`,
	}}}

	result, err := sub.Slice(time.Second, 4*time.Second)
	if err != nil {
		t.Fatalf("slice: %v", err)
	}
	if got, want := result.Subtitle.Events[0].Text,
		`{\t(1000,2000,\fade(0,0,255,0,2500,3000,3500))}x`; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

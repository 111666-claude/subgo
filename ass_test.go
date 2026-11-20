package subgo

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestASSDecode(t *testing.T) {
	input := `[Script Info]
; This is a comment
ScriptType: v4.00+
PlayResX: 384
PlayResY: 288

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hello World
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Second line\NWith newline
Dialogue: 0,1:30:45.50,1:30:50.00,Default,,0,0,0,,Long timestamp
`

	format := assFormat{}
	sub, err := format.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(sub.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(sub.Events))
	}

	tests := []struct {
		idx   int
		start time.Duration
		end   time.Duration
		text  string
	}{
		{0, 1 * time.Second, 2500 * time.Millisecond, "Hello World"},
		{1, 3 * time.Second, 4 * time.Second, "Second line\nWith newline"},
		{2, 1*time.Hour + 30*time.Minute + 45*time.Second + 500*time.Millisecond, 1*time.Hour + 30*time.Minute + 50*time.Second, "Long timestamp"},
	}

	for _, tt := range tests {
		e := sub.Events[tt.idx]
		if e.Start != tt.start {
			t.Errorf("event %d: start = %v, want %v", tt.idx, e.Start, tt.start)
		}
		if e.End != tt.end {
			t.Errorf("event %d: end = %v, want %v", tt.idx, e.End, tt.end)
		}
		if e.Text != tt.text {
			t.Errorf("event %d: text = %q, want %q", tt.idx, e.Text, tt.text)
		}
	}
}

func TestASSDecodeEmptyFile(t *testing.T) {
	format := assFormat{}
	sub, err := format.Decode(strings.NewReader(""))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(sub.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(sub.Events))
	}
}

func TestASSDecodeNoEventsSection(t *testing.T) {
	input := `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20
`

	format := assFormat{}
	sub, err := format.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(sub.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(sub.Events))
	}
}

func TestASSDecodeNewlineVariants(t *testing.T) {
	input := `[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Line1\NLine2
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Line1\nLine2
`

	format := assFormat{}
	sub, err := format.Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if len(sub.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(sub.Events))
	}

	// Both \N and \n should be converted to actual newlines
	for i, e := range sub.Events {
		if e.Text != "Line1\nLine2" {
			t.Errorf("event %d: text = %q, want %q", i, e.Text, "Line1\nLine2")
		}
	}
}

func TestASSEncode(t *testing.T) {
	sub := Subtitle{
		Events: []Event{
			{Start: 1 * time.Second, End: 2500 * time.Millisecond, Text: "Hello World"},
			{Start: 3 * time.Second, End: 4 * time.Second, Text: "Second line\nWith newline"},
		},
	}

	format := assFormat{}
	var buf bytes.Buffer
	err := format.Encode(&buf, sub)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}

	output := buf.String()

	// Check sections exist
	if !strings.Contains(output, "[Script Info]") {
		t.Error("missing [Script Info] section")
	}
	if !strings.Contains(output, "[V4+ Styles]") {
		t.Error("missing [V4+ Styles] section")
	}
	if !strings.Contains(output, "[Events]") {
		t.Error("missing [Events] section")
	}

	// Check dialogue lines
	if !strings.Contains(output, "Dialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hello World") {
		t.Errorf("first dialogue not encoded correctly:\n%s", output)
	}
	if !strings.Contains(output, "Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Second line\\NWith newline") {
		t.Errorf("second dialogue not encoded correctly (newline should be \\N):\n%s", output)
	}
}

func TestASSEncodeEmpty(t *testing.T) {
	sub := Subtitle{}

	format := assFormat{}
	var buf bytes.Buffer
	err := format.Encode(&buf, sub)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}

	output := buf.String()
	// Should still have headers even with no events
	if !strings.Contains(output, "[Script Info]") {
		t.Error("missing [Script Info] section")
	}
	if !strings.Contains(output, "[Events]") {
		t.Error("missing [Events] section")
	}
}

func TestASSRoundTrip(t *testing.T) {
	original := Subtitle{
		Events: []Event{
			{Start: 0, End: 1 * time.Second, Text: "First"},
			{Start: 1*time.Hour + 30*time.Minute + 45*time.Second + 120*time.Millisecond, End: 1*time.Hour + 30*time.Minute + 50*time.Second, Text: "Long timestamp"},
			{Start: 5 * time.Second, End: 10 * time.Second, Text: "Multi\nLine\nText"},
		},
	}

	format := assFormat{}

	// Encode
	var buf bytes.Buffer
	if err := format.Encode(&buf, original); err != nil {
		t.Fatalf("encode error: %v", err)
	}

	// Decode
	decoded, err := format.Decode(&buf)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}

	// Compare
	if len(decoded.Events) != len(original.Events) {
		t.Fatalf("event count mismatch: got %d, want %d", len(decoded.Events), len(original.Events))
	}

	for i, orig := range original.Events {
		dec := decoded.Events[i]
		// ASS uses centiseconds, so we lose some precision
		// Allow 10ms tolerance
		if abs(dec.Start-orig.Start) > 10*time.Millisecond {
			t.Errorf("event %d: start = %v, want %v", i, dec.Start, orig.Start)
		}
		if abs(dec.End-orig.End) > 10*time.Millisecond {
			t.Errorf("event %d: end = %v, want %v", i, dec.End, orig.End)
		}
		if dec.Text != orig.Text {
			t.Errorf("event %d: text = %q, want %q", i, dec.Text, orig.Text)
		}
	}
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestASSTimestampParsing(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"0:00:00.00", 0},
		{"0:00:01.00", 1 * time.Second},
		{"0:00:00.01", 10 * time.Millisecond},
		{"0:00:00.99", 990 * time.Millisecond},
		{"0:01:00.00", 1 * time.Minute},
		{"1:00:00.00", 1 * time.Hour},
		{"1:30:45.50", 1*time.Hour + 30*time.Minute + 45*time.Second + 500*time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseASSTimestamp(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestASSTimestampParsingErrors(t *testing.T) {
	tests := []string{
		"",
		"0:00:00",    // missing centiseconds
		"0:00:00,00", // wrong separator
		"invalid",
		"00:00.00", // missing hours
		"x:00:00.00",
		"0:x:00.00",
		"0:00:x.00",
		"0:00:00.x",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseASSTimestamp(input)
			if err == nil {
				t.Errorf("expected error for input %q", input)
			}
		})
	}
}

func TestASSTimestampFormatting(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "0:00:00.00"},
		{1 * time.Second, "0:00:01.00"},
		{10 * time.Millisecond, "0:00:00.01"},
		{990 * time.Millisecond, "0:00:00.99"},
		{1 * time.Minute, "0:01:00.00"},
		{1 * time.Hour, "1:00:00.00"},
		{1*time.Hour + 30*time.Minute + 45*time.Second + 500*time.Millisecond, "1:30:45.50"},
		{-1 * time.Second, "0:00:00.00"}, // negative clamped to zero
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatASSTimestamp(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestASSDialogueParsing(t *testing.T) {
	formatFields := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}

	tests := []struct {
		name      string
		input     string
		wantStart time.Duration
		wantEnd   time.Duration
		wantText  string
	}{
		{
			name:      "simple",
			input:     "0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Hello",
			wantStart: 1 * time.Second,
			wantEnd:   2 * time.Second,
			wantText:  "Hello",
		},
		{
			name:      "with commas in text",
			input:     "0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Hello, World, Test",
			wantStart: 1 * time.Second,
			wantEnd:   2 * time.Second,
			wantText:  "Hello, World, Test",
		},
		{
			name:      "with newlines",
			input:     "0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Line1\\NLine2",
			wantStart: 1 * time.Second,
			wantEnd:   2 * time.Second,
			wantText:  "Line1\nLine2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := parseASSDialogue(tt.input, formatFields)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if event.Start != tt.wantStart {
				t.Errorf("start = %v, want %v", event.Start, tt.wantStart)
			}
			if event.End != tt.wantEnd {
				t.Errorf("end = %v, want %v", event.End, tt.wantEnd)
			}
			if event.Text != tt.wantText {
				t.Errorf("text = %q, want %q", event.Text, tt.wantText)
			}
		})
	}
}

func TestASSDialogueParsingErrors(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		input  string
	}{
		{"too few fields", []string{"start", "end", "text"}, "0:00:01.00"},
		{"missing start field", []string{"end", "text"}, "0:00:02.00,hello"},
		{"invalid start timestamp", []string{"start", "end", "text"}, "bad,0:00:02.00,hello"},
		{"missing end field", []string{"start", "text"}, "0:00:01.00,hello"},
		{"invalid end timestamp", []string{"start", "end", "text"}, "0:00:01.00,bad,hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseASSDialogue(tt.input, tt.fields); err == nil {
				t.Error("expected a dialogue parsing error")
			}
		})
	}
}

func TestASSFieldsParsing(t *testing.T) {
	input := "Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text"
	fields := parseASSFields(input)

	expected := []string{"layer", "start", "end", "style", "name", "marginl", "marginr", "marginv", "effect", "text"}

	if len(fields) != len(expected) {
		t.Fatalf("got %d fields, want %d", len(fields), len(expected))
	}

	for i, f := range fields {
		if f != expected[i] {
			t.Errorf("field %d = %q, want %q", i, f, expected[i])
		}
	}
}

func TestASSDecodePreservesMetadata(t *testing.T) {
	input := "\uFEFF" + `[Script Info]
Title: Keep me
PlayResX: 1920

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20
Style: Sign,Bold,30

[Fonts]
fontname: demo.ttf

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 2,0:00:01.00,0:00:02.00,Sign,Alice,10,20,30,scroll up,Hello, World
`

	sub, err := (assFormat{}).Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if sub.ASS == nil {
		t.Fatal("ASS metadata missing")
	}
	if sub.ASS.Header != "\uFEFF" {
		t.Errorf("header = %q, want a BOM", sub.ASS.Header)
	}
	if !sub.ASS.hasSection("Script Info") || !sub.ASS.hasSection("Fonts") {
		t.Errorf("sections = %+v, want Script Info and Fonts", sub.ASS.Sections)
	}
	if got, want := sub.ASS.Styles, []string{"Default", "Sign"}; !slicesEqual(got, want) {
		t.Errorf("styles = %v, want %v", got, want)
	}
	if sub.ASS.EventFormat != "Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text" {
		t.Errorf("event format = %q", sub.ASS.EventFormat)
	}

	event := sub.Events[0]
	if event.ASS == nil {
		t.Fatal("event ASS fields missing")
	}
	if style, _ := event.ASS.Value("style"); style != "Sign" {
		t.Errorf("style = %q, want Sign", style)
	}
	if name, _ := event.ASS.Value("name"); name != "Alice" {
		t.Errorf("name = %q, want Alice", name)
	}
	if layer, _ := event.ASS.Value("layer"); layer != "2" {
		t.Errorf("layer = %q, want 2", layer)
	}
	if event.Text != "Hello, World" {
		t.Errorf("text = %q, want %q", event.Text, "Hello, World")
	}

	// Re-encoding keeps every section and the original dialogue fields.
	var buf bytes.Buffer
	if err := (assFormat{}).Encode(&buf, sub); err != nil {
		t.Fatalf("encode error: %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		"\uFEFF[Script Info]",
		"Title: Keep me",
		"PlayResX: 1920",
		"Style: Sign,Bold,30",
		"[Fonts]",
		"fontname: demo.ttf",
		"Dialogue: 2,0:00:01.00,0:00:02.00,Sign,Alice,10,20,30,scroll up,Hello, World",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
}

func TestASSDecodeKeepsNonDefaultFormatOrder(t *testing.T) {
	input := `[Events]
Format: Start, End, Text, Style
Dialogue: 0:00:01.00,0:00:02.00,Hello,Sign
`
	sub, err := (assFormat{}).Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(sub.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(sub.Events))
	}
	if sub.Events[0].Text != "Hello" {
		t.Errorf("text = %q, want Hello", sub.Events[0].Text)
	}

	var buf bytes.Buffer
	if err := (assFormat{}).Encode(&buf, sub); err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if !strings.Contains(buf.String(), "Format: Start, End, Text, Style") {
		t.Errorf("format line not preserved:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "Dialogue: 0:00:01.00,0:00:02.00,Hello,Sign") {
		t.Errorf("dialogue line not preserved:\n%s", buf.String())
	}
}

func TestShiftsPreserveASSFields(t *testing.T) {
	event := Event{
		Start: time.Second,
		End:   2 * time.Second,
		Text:  "Hello",
		ASS:   &ASSEvent{Fields: []ASSField{{Name: "style", Value: "Sign"}}},
	}
	sub := Subtitle{Events: []Event{event}, ASS: &ASSMetadata{Styles: []string{"Sign"}}}

	for name, got := range map[string]Subtitle{
		"shift":      sub.Shift(time.Second, true),
		"stretch":    sub.Stretch(2, 0),
		"remove hi":  sub.RemoveHI(),
		"remove ids": sub.RemoveIds(nil),
	} {
		if got.Events[0].ASS == nil {
			t.Errorf("%s: ASS fields dropped", name)
		}
		if got.ASS == nil {
			t.Errorf("%s: ASS metadata dropped", name)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDeclaredStylesWithoutFormatLine(t *testing.T) {
	input := `[V4+ Styles]
Style: First,Arial,20
Style: Second,Arial,20

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Second,,0,0,0,,Hello
`
	sub, err := (assFormat{}).Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, want := sub.ASS.Styles, []string{"First", "Second"}; !slicesEqual(got, want) {
		t.Errorf("styles = %v, want %v", got, want)
	}
}

func TestASSEncodeUsesPreservedEventsFormat(t *testing.T) {
	input := `[Events]
Format: Start, End, Text, Style
`
	sub, err := (assFormat{}).Decode(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var buf bytes.Buffer
	if err := (assFormat{}).Encode(&buf, sub); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), "Format: Start, End, Text, Style") {
		t.Errorf("events format not preserved:\n%s", buf.String())
	}
}

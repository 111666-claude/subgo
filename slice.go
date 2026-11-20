package subgo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Diagnostic codes reported by Slice.
const (
	// CodeEmptySlice reports that the requested range contains no events.
	CodeEmptySlice = "empty-slice"
	// CodeReversedRange reports that the range end precedes its start.
	CodeReversedRange = "reversed-range"
	// CodeNegativeRange reports a negative range start.
	CodeNegativeRange = "negative-range"
	// CodeMissingStyle reports an event style that the script never declares.
	CodeMissingStyle = "missing-style"
	// CodeDuplicateEvent reports two output events with identical content.
	CodeDuplicateEvent = "duplicate-event"
	// CodeOutOfRangeTag reports an animation tag whose times fall outside the
	// clipped event.
	CodeOutOfRangeTag = "out-of-range-tag"
	// CodeUnsafeTag reports an animation tag that cannot be retimed safely.
	CodeUnsafeTag = "unsafe-tag"
)

// UnsafeTagPolicy controls how animation tags that cannot be retimed safely
// are handled.
type UnsafeTagPolicy int

const (
	// UnsafeTagPreserve keeps the original tag and reports a diagnostic.
	UnsafeTagPreserve UnsafeTagPolicy = iota
	// UnsafeTagError aborts the slice with a *SliceError.
	UnsafeTagError
)

// SliceOptions configures SliceWithOptions. The zero value preserves unsafe
// tags.
type SliceOptions struct {
	UnsafeTagPolicy UnsafeTagPolicy
}

// Diagnostic describes a non-fatal condition found while slicing. EventIndex
// is zero-based and is -1 when the condition is not tied to an event.
type Diagnostic struct {
	Code       string
	EventIndex int
	// Offset is the byte offset of the offending tag within the event text.
	Offset  int
	Tag     string
	Style   string
	Message string
}

// SliceError is returned when a slice cannot be produced. EventIndex is
// zero-based and is -1 when the problem concerns the whole range.
type SliceError struct {
	Code       string
	EventIndex int
	Offset     int
	Tag        string
	Message    string
}

func (e *SliceError) Error() string {
	location := "slice"
	if e.EventIndex >= 0 {
		location += fmt.Sprintf(": event %d", e.EventIndex+1)
		if e.Offset >= 0 {
			location += fmt.Sprintf(" offset %d", e.Offset)
		}
	}
	return location + " (" + e.Code + "): " + e.Message
}

// SliceResult is the output of Slice.
type SliceResult struct {
	Subtitle    Subtitle
	Diagnostics []Diagnostic
}

// Slice extracts the events intersecting [start, end) and rebases their times
// to zero. Events crossing a boundary are clipped to it. ASS/SSA script
// metadata, styles and dialogue fields are preserved, and relative animation
// tag times are adjusted for a clipped cue head.
//
// end must not precede start, and start must not be negative. An empty range
// is reported as a CodeEmptySlice diagnostic rather than an error. Animation
// tags that cannot be retimed safely are preserved and reported as
// CodeUnsafeTag diagnostics; use SliceWithOptions to make them fatal instead.
func (s Subtitle) Slice(start, end time.Duration) (SliceResult, error) {
	return s.SliceWithOptions(start, end, SliceOptions{})
}

// SliceWithOptions is Slice with an explicit UnsafeTagPolicy.
func (s Subtitle) SliceWithOptions(start, end time.Duration, opts SliceOptions) (SliceResult, error) {
	if start < 0 {
		return SliceResult{}, &SliceError{
			Code:       CodeNegativeRange,
			EventIndex: -1,
			Offset:     -1,
			Message:    fmt.Sprintf("range start %v is negative", start),
		}
	}
	if end < start {
		return SliceResult{}, &SliceError{
			Code:       CodeReversedRange,
			EventIndex: -1,
			Offset:     -1,
			Message:    fmt.Sprintf("range end %v precedes range start %v", end, start),
		}
	}

	result := SliceResult{Subtitle: Subtitle{ASS: s.ASS.clone()}}
	seen := make(map[string]int, len(s.Events))

	for index, event := range s.Events {
		if event.End <= start || event.Start >= end {
			continue
		}

		clipped, diagnostics, err := sliceEvent(index, event, start, end, opts)
		if err != nil {
			return SliceResult{}, err
		}
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		result.Diagnostics = append(result.Diagnostics, s.missingStyleDiagnostic(index, event)...)

		if first, exists := seen[sliceEventKey(clipped)]; exists {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Code:       CodeDuplicateEvent,
				EventIndex: index,
				Offset:     -1,
				Message:    fmt.Sprintf("event %d duplicates event %d", index+1, first+1),
			})
		} else {
			seen[sliceEventKey(clipped)] = index
		}

		result.Subtitle.Events = append(result.Subtitle.Events, clipped)
	}

	if len(result.Subtitle.Events) == 0 {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:       CodeEmptySlice,
			EventIndex: -1,
			Offset:     -1,
			Message:    fmt.Sprintf("range %v-%v contains no events", start, end),
		})
	}

	return result, nil
}

func (s Subtitle) missingStyleDiagnostic(index int, event Event) []Diagnostic {
	if s.ASS == nil || event.ASS == nil {
		return nil
	}
	style := event.ASS.Style()
	if containsString(s.ASS.Styles, style) {
		return nil
	}
	return []Diagnostic{{
		Code:       CodeMissingStyle,
		EventIndex: index,
		Offset:     -1,
		Style:      style,
		Message:    fmt.Sprintf("style %q is not declared by the script", style),
	}}
}

func sliceEvent(index int, event Event, start, end time.Duration, opts SliceOptions) (Event, []Diagnostic, error) {
	newStart := event.Start
	if newStart < start {
		newStart = start
	}
	newEnd := event.End
	if newEnd > end {
		newEnd = end
	}
	headCut := newStart - event.Start

	clipped := Event{
		Start: newStart - start,
		End:   newEnd - start,
		Text:  event.Text,
		ASS:   event.ASS.clone(),
	}

	text, findings := retimeASSText(event.Text, headCut, event.End-event.Start, clipped.End-clipped.Start)
	clipped.Text = text
	if clipped.ASS != nil && text != event.Text {
		clipped.ASS = clipped.ASS.WithField("text", strings.ReplaceAll(text, "\n", "\\N"))
	}

	var diagnostics []Diagnostic
	for _, finding := range findings {
		if finding.code == CodeUnsafeTag && opts.UnsafeTagPolicy == UnsafeTagError {
			return Event{}, nil, &SliceError{
				Code:       finding.code,
				EventIndex: index,
				Offset:     finding.offset,
				Tag:        finding.tag,
				Message:    finding.message,
			}
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:       finding.code,
			EventIndex: index,
			Offset:     finding.offset,
			Tag:        finding.tag,
			Message:    finding.message,
		})
	}
	return clipped, diagnostics, nil
}

func sliceEventKey(event Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%d|%s", event.Start, event.End, event.Text)
	if event.ASS == nil {
		return b.String()
	}
	for _, field := range event.ASS.Fields {
		switch strings.ToLower(field.Name) {
		case "start", "end", "text":
			continue
		}
		b.WriteString("|")
		b.WriteString(strings.ToLower(field.Name))
		b.WriteString("=")
		b.WriteString(field.Value)
	}
	return b.String()
}

type tagFinding struct {
	code    string
	tag     string
	offset  int
	message string
}

// retimeASSText adjusts time-bearing override tags for a clipped cue. headCut
// is how much was removed from the cue head, origDuration is the source cue
// duration and newDuration is the clipped cue duration.
func retimeASSText(text string, headCut, origDuration, newDuration time.Duration) (string, []tagFinding) {
	var out strings.Builder
	var findings []tagFinding
	cursor := 0
	for {
		open := strings.IndexByte(text[cursor:], '{')
		if open < 0 {
			out.WriteString(text[cursor:])
			break
		}
		open += cursor
		if open > 0 && text[open-1] == '\\' {
			// An escaped brace is literal text, not an override block.
			out.WriteString(text[cursor : open+1])
			cursor = open + 1
			continue
		}
		closeIndex := strings.IndexByte(text[open:], '}')
		if closeIndex < 0 {
			out.WriteString(text[cursor:])
			break
		}
		closeIndex += open

		out.WriteString(text[cursor : open+1])
		block, blockFindings := retimeASSBlock(text[open+1:closeIndex], open+1, headCut, origDuration, newDuration)
		out.WriteString(block)
		out.WriteByte('}')
		findings = append(findings, blockFindings...)
		cursor = closeIndex + 1
	}
	return out.String(), findings
}

func retimeASSBlock(block string, baseOffset int, headCut, origDuration, newDuration time.Duration) (string, []tagFinding) {
	tokens := splitASSTokens(block)
	var out strings.Builder
	var findings []tagFinding
	for _, token := range tokens {
		rewritten, tokenFindings := retimeASSToken(token, baseOffset, headCut, origDuration, newDuration)
		out.WriteString(rewritten)
		findings = append(findings, tokenFindings...)
	}
	return out.String(), findings
}

type assToken struct {
	offset int
	text   string
}

// splitASSTokens splits an override block into top-level tags, keeping nested
// argument lists such as \t(0,500,\frz10\bord0) in one piece.
func splitASSTokens(block string) []assToken {
	var tokens []assToken
	start := 0
	depth := 0
	for i := 0; i < len(block); i++ {
		switch block[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '\\':
			if depth == 0 {
				tokens = append(tokens, assToken{offset: start, text: block[start:i]})
				start = i
			}
		}
	}
	tokens = append(tokens, assToken{offset: start, text: block[start:]})
	return tokens
}

func retimeASSToken(token assToken, baseOffset int, headCut, origDuration, newDuration time.Duration) (string, []tagFinding) {
	if !strings.HasPrefix(token.text, "\\") {
		return token.text, nil
	}
	name, argsText, ok := parseASSTag(token.text)
	if !ok {
		return token.text, nil
	}
	offset := baseOffset + token.offset

	switch strings.ToLower(name) {
	case "move":
		return retimeASSMove(token.text, argsText, offset, headCut, origDuration, newDuration)
	case "fade":
		return retimeASSFade(token.text, argsText, offset, headCut, newDuration)
	case "fad":
		return diagnoseASSFad(token.text, argsText, offset, newDuration)
	case "t":
		return retimeASSTransform(token.text, argsText, offset, headCut, newDuration)
	case "kt":
		return retimeASSTimeTag(token.text, argsText, offset, "kt", headCut, newDuration)
	case "k", "kf", "ko":
		if headCut <= 0 {
			return token.text, nil
		}
		return token.text, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\" + name,
			offset:  offset,
			message: "karaoke timings cannot be retimed after clipping the cue head",
		}}
	default:
		return token.text, nil
	}
}

func parseASSTag(token string) (string, string, bool) {
	body := strings.TrimPrefix(token, "\\")
	end := 0
	for end < len(body) && (body[end] >= 'a' && body[end] <= 'z' || body[end] >= 'A' && body[end] <= 'Z') {
		end++
	}
	name, rest := body[:end], body[end:]
	if rest == "" {
		return name, "", name != ""
	}
	if rest[0] != '(' {
		return name, rest, name != ""
	}
	if !strings.HasSuffix(rest, ")") {
		return "", "", false
	}
	return name, rest[1 : len(rest)-1], true
}

func retimeASSMove(token, argsText string, offset int, headCut, origDuration, newDuration time.Duration) (string, []tagFinding) {
	args := splitASTopLevel(argsText)
	switch len(args) {
	case 4:
		if headCut <= 0 {
			// A four-argument \move spans the whole cue, so a tail clip alone
			// already retimes it correctly.
			return token, nil
		}
		return materializeASSMove(token, args, offset, headCut, origDuration, newDuration)
	case 6:
		return retimeASSMoveTimes(token, args, offset, headCut, newDuration)
	default:
		if headCut <= 0 {
			return token, nil
		}
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\move",
			offset:  offset,
			message: "unrecognised \\move arguments",
		}}
	}
}

// materializeASSMove turns an implicit-duration move into an explicit one that
// continues from the position reached at the clipped cue head instead of
// restarting at the original start position.
func materializeASSMove(token string, args []string, offset int, headCut, origDuration, newDuration time.Duration) (string, []tagFinding) {
	x1, err1 := strconv.ParseFloat(strings.TrimSpace(args[0]), 64)
	y1, err2 := strconv.ParseFloat(strings.TrimSpace(args[1]), 64)
	x2, err3 := strconv.ParseFloat(strings.TrimSpace(args[2]), 64)
	y2, err4 := strconv.ParseFloat(strings.TrimSpace(args[3]), 64)
	if err := firstError(err1, err2, err3, err4); err != nil {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\move",
			offset:  offset,
			message: "cannot parse \\move coordinates: " + err.Error(),
		}}
	}
	if origDuration <= 0 || newDuration <= 0 {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\move",
			offset:  offset,
			message: "\\move has no usable duration to retime",
		}}
	}

	fraction := float64(headCut) / float64(origDuration)
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	xStart := x1 + (x2-x1)*fraction
	yStart := y1 + (y2-y1)*fraction

	return fmt.Sprintf(`\move(%s,%s,%s,%s,0,%d)`,
		formatASSNumber(xStart), formatASSNumber(yStart),
		formatASSNumber(x2), formatASSNumber(y2),
		newDuration.Milliseconds()), nil
}

func retimeASSMoveTimes(token string, args []string, offset int, headCut, newDuration time.Duration) (string, []tagFinding) {
	t1, err1 := strconv.Atoi(strings.TrimSpace(args[4]))
	t2, err2 := strconv.Atoi(strings.TrimSpace(args[5]))
	if err := firstError(err1, err2); err != nil {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\move",
			offset:  offset,
			message: "cannot parse \\move times: " + err.Error(),
		}}
	}

	cut := int(headCut.Milliseconds())
	newT1, newT2 := t1-cut, t2-cut
	if newT1 < 0 || newT2 <= 0 {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\move",
			offset:  offset,
			message: fmt.Sprintf("\\move starts before the clipped cue head (t1=%dms, t2=%dms)", t1, t2),
		}}
	}

	if cut == 0 {
		return token, outOfRangeFinding("\\move", offset, newDuration, t1, t2)
	}
	rewritten := fmt.Sprintf(`\move(%s,%s,%s,%s,%d,%d)`,
		args[0], args[1], args[2], args[3], newT1, newT2)
	return rewritten, outOfRangeFinding("\\move", offset, newDuration, newT1, newT2)
}

func retimeASSFade(token, argsText string, offset int, headCut, newDuration time.Duration) (string, []tagFinding) {
	args := splitASTopLevel(argsText)
	if len(args) != 7 {
		return token, nil
	}

	times := make([]int, 4)
	for i := range times {
		value, err := strconv.Atoi(strings.TrimSpace(args[3+i]))
		if err != nil {
			return token, []tagFinding{{
				code:    CodeUnsafeTag,
				tag:     "\\fade",
				offset:  offset,
				message: "cannot parse \\fade times",
			}}
		}
		times[i] = value
	}

	cut := int(headCut.Milliseconds())
	for _, t := range times {
		if t-cut < 0 {
			return token, []tagFinding{{
				code:    CodeUnsafeTag,
				tag:     "\\fade",
				offset:  offset,
				message: "\\fade starts before the clipped cue head",
			}}
		}
	}

	var rewritten strings.Builder
	rewritten.WriteString(`\fade(`)
	for i, arg := range args {
		if i > 0 {
			rewritten.WriteByte(',')
		}
		if i >= 3 {
			times[i-3] -= cut
			rewritten.WriteString(strconv.Itoa(times[i-3]))
			continue
		}
		rewritten.WriteString(arg)
	}
	rewritten.WriteByte(')')

	return rewritten.String(), outOfRangeFinding("\\fade", offset, newDuration, times...)
}

func diagnoseASSFad(token, argsText string, offset int, newDuration time.Duration) (string, []tagFinding) {
	args := splitASTopLevel(argsText)
	if len(args) != 2 {
		return token, nil
	}
	in, err1 := strconv.Atoi(strings.TrimSpace(args[0]))
	out, err2 := strconv.Atoi(strings.TrimSpace(args[1]))
	if err := firstError(err1, err2); err != nil {
		return token, nil
	}
	if time.Duration(in+out)*time.Millisecond > newDuration {
		return token, []tagFinding{{
			code:    CodeOutOfRangeTag,
			tag:     "\\fad",
			offset:  offset,
			message: fmt.Sprintf("\\fad(%d,%d) is longer than the clipped cue (%dms)", in, out, newDuration.Milliseconds()),
		}}
	}
	return token, nil
}

func retimeASSTransform(token, argsText string, offset int, headCut, newDuration time.Duration) (string, []tagFinding) {
	args := splitASTopLevel(argsText)
	if len(args) < 2 {
		return token, nil
	}
	t1, err1 := strconv.Atoi(strings.TrimSpace(args[0]))
	t2, err2 := strconv.Atoi(strings.TrimSpace(args[1]))
	if err := firstError(err1, err2); err != nil {
		return token, nil
	}

	cut := int(headCut.Milliseconds())
	if cut == 0 {
		return token, outOfRangeFinding("\\t", offset, newDuration, t1, t2)
	}
	newT1, newT2 := t1-cut, t2-cut
	if newT1 < 0 || newT2 < 0 {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\t",
			offset:  offset,
			message: fmt.Sprintf("\\t starts before the clipped cue head (t1=%dms, t2=%dms)", t1, t2),
		}}
	}

	var rewritten strings.Builder
	rewritten.WriteString(`\t(`)
	for i, arg := range args {
		if i > 0 {
			rewritten.WriteByte(',')
		}
		switch i {
		case 0:
			rewritten.WriteString(strconv.Itoa(newT1))
		case 1:
			rewritten.WriteString(strconv.Itoa(newT2))
		default:
			rewritten.WriteString(arg)
		}
	}
	rewritten.WriteByte(')')

	return rewritten.String(), outOfRangeFinding("\\t", offset, newDuration, newT1, newT2)
}

func retimeASSTimeTag(token, argsText string, offset int, tag string, headCut, newDuration time.Duration) (string, []tagFinding) {
	value, err := strconv.Atoi(strings.TrimSpace(argsText))
	if err != nil {
		return token, nil
	}
	newValue := value - int(headCut.Milliseconds())
	if newValue < 0 {
		return token, []tagFinding{{
			code:    CodeUnsafeTag,
			tag:     "\\" + tag,
			offset:  offset,
			message: fmt.Sprintf("\\%s starts before the clipped cue head", tag),
		}}
	}
	if headCut <= 0 {
		return token, outOfRangeFinding("\\"+tag, offset, newDuration, value)
	}
	return fmt.Sprintf(`\%s(%d)`, tag, newValue), outOfRangeFinding("\\"+tag, offset, newDuration, newValue)
}

// outOfRangeFinding reports times that fall past the clipped cue end.
func outOfRangeFinding(tag string, offset int, newDuration time.Duration, times ...int) []tagFinding {
	limit := int(newDuration.Milliseconds())
	for _, t := range times {
		if t > limit {
			return []tagFinding{{
				code:    CodeOutOfRangeTag,
				tag:     tag,
				offset:  offset,
				message: fmt.Sprintf("%s time %dms is past the clipped cue end (%dms)", tag, t, limit),
			}}
		}
	}
	return nil
}

func splitASTopLevel(s string) []string {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func formatASSNumber(value float64) string {
	if math.Trunc(value) == value {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64)
}

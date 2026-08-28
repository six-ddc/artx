package eventlog

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/six-ddc/artx/internal/anchor"
	"github.com/six-ddc/artx/internal/api"
)

// Marshal's contract: whatever text lands in an event — comment bodies and
// anchor quotes are arbitrary document excerpts — the emitted block MUST
// parse back identically. The corpus below is built from YAML's own failure
// modes; the real-world trigger was an anchor quote around indented code
// next to a ``` fence (a literal block whose first content line is empty
// and whose later lines carry leading spaces loses its indentation
// indicator on encode).
var nastyStrings = []string{
	"\n    // 持有 flock 直接写\n}\n```\n\n这样做的", // the original corruption
	" c != nil {\n    // 走 HTTP API\n} ",
	"```go\nfunc main() {}\n```",
	"    four leading spaces",
	"\tleading tab\n\ttabbed line",
	"\n\n\n",
	"line\r\nwith crlf\r\n",
	"---\nlooks like a document separator\n---",
	"...\ndocument end marker",
	"key: value  # looks like yaml",
	"- looks like a list\n- second item",
	"| pipe\n> folded\n& anchor\n* alias\n! tag\n% directive\n@ at\n` backtick",
	"'single' and \"double\" quotes",
	"#comment-looking",
	"?: mapping indicators",
	"emoji 🀄🚀 and 中文，混排 ✅",
	"trailing spaces   \nmore   ",
	"", // empty is dropped by omitempty; still must not corrupt anything
}

func TestMarshalRoundTripsArbitraryContent(t *testing.T) {
	for i, s := range nastyStrings {
		t.Run(fmt.Sprintf("case_%02d", i), func(t *testing.T) {
			e := Event{
				E: KindCreate, EID: fmt.Sprintf("e%02d", i), TS: time.Now().Round(time.Second),
				Thread: "cabcde", Author: "u", Body: s,
				Anchor: &anchor.Anchor{
					Kind: api.AnchorText, Exact: s, Prefix: s, Suffix: s,
					Start: 1, End: 2,
				},
			}
			data, err := Marshal(e)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			evs, report, err := ReadFrom(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("ReadFrom: %v\nyaml:\n%s", err, data)
			}
			if report != nil && len(report.Warnings) > 0 {
				t.Fatalf("warnings: %v\nyaml:\n%s", report.Warnings, data)
			}
			if len(evs) != 1 {
				t.Fatalf("got %d events\nyaml:\n%s", len(evs), data)
			}
			got := evs[0]
			if got.Body != s {
				t.Fatalf("Body mangled: %q -> %q\nyaml:\n%s", s, got.Body, data)
			}
			if got.Anchor == nil || got.Anchor.Exact != s || got.Anchor.Prefix != s || got.Anchor.Suffix != s {
				t.Fatalf("Anchor mangled for %q: %+v\nyaml:\n%s", s, got.Anchor, data)
			}
		})
	}
}

// A batch mixing pretty-form and fallback-form events must still split into
// the right number of blocks — content containing "---" lines is the case
// that would break the block splitter if it ever leaked unindented.
func TestMarshalBatchWithSeparatorContent(t *testing.T) {
	var events []Event
	for i, s := range nastyStrings {
		events = append(events, Event{
			E: KindReply, EID: fmt.Sprintf("r%02d", i), TS: time.Now().Round(time.Second),
			Thread: "cabcde", ID: fmt.Sprintf("cabcde.%03d", i), Author: "u", Body: s,
		})
	}
	data, err := Marshal(events...)
	if err != nil {
		t.Fatal(err)
	}
	evs, report, err := ReadFrom(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if report != nil && len(report.Warnings) > 0 {
		t.Fatalf("warnings: %v", report.Warnings)
	}
	if len(evs) != len(events) {
		t.Fatalf("got %d events, want %d", len(evs), len(events))
	}
	for i, got := range evs {
		if got.Body != events[i].Body {
			t.Fatalf("event %d body mangled: %q -> %q", i, events[i].Body, got.Body)
		}
	}
}

package eventlog

import (
	"testing"

	"github.com/six-ddc/artx/internal/api"
)

func TestCompactMessage(t *testing.T) {
	cases := []struct {
		name  string
		stats []api.CompactStat
		want  string
	}{
		{
			"single doc",
			[]api.CompactStat{{Doc: "a7f3", EventsBefore: 120, EventsAfter: 8, ThreadsArchived: 3}},
			"artx: compact a7f3 (120 -> 8 events, 3 threads archived)",
		},
		{
			"single doc, one thread",
			[]api.CompactStat{{Doc: "a7f3", EventsBefore: 20, EventsAfter: 5, ThreadsArchived: 1}},
			"artx: compact a7f3 (20 -> 5 events, 1 thread archived)",
		},
		{
			"nothing archived omits the segment",
			[]api.CompactStat{{Doc: "a7f3", EventsBefore: 30, EventsAfter: 12}},
			"artx: compact a7f3 (30 -> 12 events)",
		},
		{
			"multiple docs aggregate, skipped excluded",
			[]api.CompactStat{
				{Doc: "a7f3", EventsBefore: 120, EventsAfter: 8, ThreadsArchived: 3},
				{Doc: "b2k9", Skipped: true, EventsBefore: 999},
				{Doc: "c4m1", EventsBefore: 130, EventsAfter: 32, ThreadsArchived: 4},
			},
			"artx: compact 2 docs (250 -> 40 events, 7 threads archived)",
		},
		{
			"all skipped",
			[]api.CompactStat{{Doc: "a7f3", Skipped: true}},
			"artx: compact vault",
		},
	}
	for _, tc := range cases {
		if got := CompactMessage(tc.stats); got != tc.want {
			t.Errorf("%s: CompactMessage = %q, want %q", tc.name, got, tc.want)
		}
	}
}

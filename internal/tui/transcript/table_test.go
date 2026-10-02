package transcript

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"agents/internal/app"

	"github.com/charmbracelet/x/ansi"
)

func TestTableLayout(t *testing.T) {
	const markdown = `Before the table.

| Harness | Transport | Status |
| :--- | :---: | ---: |
| OpenCode | ACP stdio | Ready |
| Claude Agent | ACP bridge | Planned |
| Codex | ACP bridge | Planned |

After the table.`
	c := &app.Conversation{Entries: []*app.Entry{{Kind: "message", Role: "assistant", Text: markdown, Revision: 1}}}
	for _, tc := range []struct {
		name  string
		width int
	}{{"wide", 80}, {"narrow", 40}} {
		t.Run(tc.name, func(t *testing.T) {
			rendered, _ := New().Render(c, Options{Width: tc.width})
			assertTableFits(t, rendered, tc.width)
			for _, line := range strings.Split(rendered, "\n") {
				if strings.Contains(ansi.Strip(line), "Harness") && !regexp.MustCompile(`\x1b\[(?:[0-9]*;)*1(?:;[0-9]*)*m`).MatchString(line) {
					t.Fatal("table header has no bold emphasis")
				}
			}
			want, err := os.ReadFile(filepath.Join("testdata", "table-"+tc.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if got := normalizedTable(rendered); got != strings.TrimSpace(string(want)) {
				t.Fatalf("table layout changed:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestTableRowsRemainDistinctWhenCellsWrap(t *testing.T) {
	const source = `| Feature | Description |
| --- | --- |
| Files | Read text safely inside the workspace and retain every word of this long description. |
| Tools | Run commands and inspect output. |`
	for _, width := range []int{32, 40, 60, 100} {
		rendered, err := newMarkdownRenderer(width).Render(source)
		if err != nil {
			t.Fatal(err)
		}
		assertTableFits(t, rendered, width)
		lines := strings.Split(ansi.Strip(rendered), "\n")
		rules := 0
		for _, line := range lines {
			if strings.Contains(line, "─┼") {
				rules++
			}
		}
		if rules != 2 {
			t.Fatalf("expected header and body-row rules at width %d, got %d", width, rules)
		}
		for _, word := range []string{"safely", "workspace", "retain", "every", "description.", "Tools", "output."} {
			if !strings.Contains(strings.Join(lines, "\n"), word) {
				t.Fatalf("table lost %q at width %d", word, width)
			}
		}
	}
}

func TestTableRulesPreserveEmptyRowsAndNeverLeakMarkers(t *testing.T) {
	for _, source := range []string{
		"| One |\n| --- |\n| alpha |\n| beta |",
		"| One | Two |\n| --- | --- |\n| alpha | first |\n| | |\n| beta | second |",
		"Literal \u2063 marker.\n\n| One | Two |\n| --- | --- |\n| alpha | first |\n| beta | second |",
	} {
		r := newMarkdownRenderer(40)
		rendered, err := r.Render(source)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered, r.tables.marker) {
			t.Fatal("injected table marker leaked into output")
		}
		assertTableFits(t, rendered, 40)
		plain := ansi.Strip(rendered)
		rules := 0
		for _, line := range strings.Split(plain, "\n") {
			if strings.Contains(line, "─") {
				rules++
			}
		}
		wantRules := 2
		if strings.Contains(source, "| | |") {
			wantRules = 3
		}
		if rules != wantRules {
			t.Fatalf("expected %d logical row rules, got %d", wantRules, rules)
		}
		if !strings.Contains(plain, "alpha") || !strings.Contains(plain, "beta") {
			t.Fatal("table content lost")
		}
		if strings.Contains(source, "| | |") {
			found := false
			for _, line := range strings.Split(plain, "\n") {
				if strings.Contains(line, "│") && strings.TrimSpace(strings.ReplaceAll(line, "│", "")) == "" {
					found = true
				}
			}
			if !found {
				t.Fatal("real empty table row was replaced by a rule")
			}
		}
	}
}

func TestTablesPreserveInlineMarkdownEscapesAndUnicode(t *testing.T) {
	const source = `| Item | Details |
| --- | --- |
| **OpenCode** | Use ` + "`go test`" + ` and [docs](https://example.com/docs). |
| Café 界 | left\|right &amp; escaped |
| Empty | |`
	rendered, err := newMarkdownRenderer(80).Render(source)
	if err != nil {
		t.Fatal(err)
	}
	assertTableFits(t, rendered, 80)
	plain := ansi.Strip(rendered)
	for _, want := range []string{"OpenCode", "go test", "docs", "https://example.com/docs", "Café 界", "left|right", "& escaped", "Empty"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("table lost %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "**") || strings.Contains(plain, "<table>") {
		t.Fatalf("table leaked raw markup:\n%s", plain)
	}
}

func TestAdjacentAndNestedTablesRetainTheirContext(t *testing.T) {
	const source = `| First | Value |
| --- | --- |
| alpha | one |
| beta | two |

Text between tables.

> | Second | Value |
> | --- | --- |
> | gamma | three |
> | delta | four |

Text after tables.`
	renderer := newMarkdownRenderer(80)
	first, err := renderer.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderer.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("repeated renders accumulate table spacing")
	}
	plain := ansi.Strip(first)
	previous := -1
	for _, want := range []string{"alpha", "beta", "Text between tables.", "gamma", "delta", "Text after tables."} {
		index := strings.Index(plain, want)
		if index <= previous {
			t.Fatalf("table context lost %q:\n%s", want, plain)
		}
		previous = index
	}
}

func TestTableSpacingNeverChangesPipesInsideCodeBlocks(t *testing.T) {
	const source = "```text\n| Header | Value |\n| --- | --- |\n| alpha | one |\n| beta | two |\n```"
	rendered, err := newMarkdownRenderer(80).Render(source)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	if !strings.Contains(plain, "| alpha | one |") || !strings.Contains(plain, "| beta | two |") || strings.Contains(plain, "│") {
		t.Fatalf("table formatting changed a code fence:\n%s", plain)
	}
}

func normalizedTable(rendered string) string {
	lines := strings.Split(ansi.Strip(rendered), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func assertTableFits(t *testing.T, rendered string, width int) {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("table line is %d cells wide, exceeds %d: %q", got, width, ansi.Strip(line))
		}
	}
}

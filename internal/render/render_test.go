package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/six-ddc/art/internal/mdsrc"
)

const sample = `---
aid: a7f3k2
title: 支付重构
---

# 标题一

第一段文字，包含 **粗体** 内容。
第二行继续。

> 引用块第一行
> 引用块第二行

- 列表项 A
- 列表项 B

` + "```mermaid\ngraph TD; A-->B;\n```" + `

行内公式 $x^2$ 与本段无关的代码：

` + "```\n$not-math$\n```" + `

最后一段。
`

func sourcePosAttr(s, e int) string {
	return fmt.Sprintf(`data-sourcepos="%d:%d"`, s, e)
}

// data-sourcepos must match mdsrc.Parse's block table block-for-block
// (including the frontmatter offset).
func TestRenderSourcePosMatchesBlockTable(t *testing.T) {
	res, err := New().Render([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if res.BodyOffset <= 0 {
		t.Fatalf("BodyOffset should be greater than 0 (frontmatter present), got %d", res.BodyOffset)
	}

	independent, err := mdsrc.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(independent.Blocks) == 0 {
		t.Fatal("block table is empty")
	}

	// render.Result.Doc must be the same block table an independent call to
	// mdsrc.Parse produces (same single source of truth).
	if len(res.Doc.Blocks) != len(independent.Blocks) {
		t.Fatalf("Result.Doc block count differs from independent Parse: %d vs %d", len(res.Doc.Blocks), len(independent.Blocks))
	}

	var checked int
	for _, b := range independent.Blocks {
		switch b.Kind {
		case "Heading", "Paragraph", "List", "Blockquote", "FencedCodeBlock":
			if b.Start >= b.End {
				continue
			}
			want := sourcePosAttr(b.Start, b.End)
			if !strings.Contains(res.HTML, want) {
				t.Fatalf("HTML is missing sourcepos %s matching the block table (block %s %d:%d)\nHTML:\n%s", want, b.Kind, b.Start, b.End, res.HTML)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no block was checked; the test sample is broken")
	}

	// Block-level offsets must be file-absolute: slicing the original file
	// directly must yield the expected content.
	for i := range independent.Blocks {
		b := &independent.Blocks[i]
		if b.Kind == "Heading" && b.Level == 1 {
			if got := string([]byte(sample)[b.Start:b.End]); got != "标题一" {
				t.Fatalf("h1 absolute offset is misaligned: %q", got)
			}
		}
	}
}

func TestHasMermaid(t *testing.T) {
	res, err := New().Render([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasMermaid {
		t.Fatal("sample contains a ```mermaid code block, HasMermaid should be true")
	}

	res2, err := New().Render([]byte("# 无图表\n\n普通段落。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res2.HasMermaid {
		t.Fatal("HasMermaid should be false when there is no mermaid code block")
	}
}

func TestHasMath(t *testing.T) {
	res, err := New().Render([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasMath {
		t.Fatal("body contains $x^2$, HasMath should be true")
	}

	res2, err := New().Render([]byte("# 无公式\n\n普通段落，没有美元符号。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res2.HasMath {
		t.Fatal("HasMath should be false when the body has no $")
	}
}

func TestTitleFromFrontmatterOrHeading(t *testing.T) {
	res, err := New().Render([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "支付重构" {
		t.Fatalf("should take the frontmatter title, got %q", res.Title)
	}

	res2, err := New().Render([]byte("# 只有标题\n\n正文。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Title != "只有标题" {
		t.Fatalf("should fall back to the first h1 when there is no frontmatter, got %q", res2.Title)
	}
}

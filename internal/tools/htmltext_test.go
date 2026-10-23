package tools

import (
	"strings"
	"testing"
	"time"
)

func TestHTMLToText(t *testing.T) {
	page := `<!DOCTYPE html><html><head><title>My Page</title>` +
		`<style>.x{color:red}</style><script>alert("SCRIPTX")</script></head>` +
		`<body><p>Hello &amp; welcome</p><div>second   line</div><!-- COMMENTX --></body></html>`
	tests := []struct {
		name    string
		in      string
		want    string   // exact result, when non-empty
		has     []string // substrings the result must contain
		hasNot  []string // substrings the result must not contain
		allowLT bool     // the result may contain '<' or '>'
	}{
		{
			name:   "page",
			in:     page,
			want:   "My Page\nHello & welcome\nsecond line",
			has:    []string{"Hello & welcome\nsecond line"},
			hasNot: []string{"SCRIPTX", "color:red", "COMMENTX", "DOCTYPE"},
		},
		{
			name:   "upper-case script with spaced closing tag",
			in:     `a<SCRIPT type="text/javascript">var SCRIPTX = 1 < 2;</SCRIPT >b`,
			want:   "ab",
			hasNot: []string{"SCRIPTX"},
		},
		{
			name:   "script containing other closing tags",
			in:     `x<script>document.write("</div>SCRIPTX")</script>y`,
			want:   "xy",
			hasNot: []string{"SCRIPTX"},
		},
		{
			name:   "noscript body",
			in:     `<p>keep</p><noscript><p>NOSCRIPTX</p></noscript><p>also</p>`,
			want:   "keep\nalso",
			hasNot: []string{"NOSCRIPTX"},
		},
		{
			name:   "template body",
			in:     `<div>keep</div><Template id="t"><span>TEMPLATEX</span></TEMPLATE>after`,
			want:   "keep\nafter",
			hasNot: []string{"TEMPLATEX"},
		},
		{
			name:   "style body",
			in:     `before<style media="screen">.x{color:red} p > a {}</style>after`,
			want:   "beforeafter",
			hasNot: []string{"color:red"},
		},
		{
			name:   "unclosed script drops the rest",
			in:     `visible<script>alert("SCRIPTX") <p>more SCRIPTX`,
			want:   "visible",
			hasNot: []string{"SCRIPTX"},
		},
		{
			name:   "script closing-tag prefix is not a close",
			in:     `a<script>x</scripts>SCRIPTX</script>b`,
			want:   "ab",
			hasNot: []string{"SCRIPTX"},
		},
		{
			name:   "comment dropped",
			in:     `one<!-- COMMENTX <p> -->two<!---->three<!-->four`,
			want:   "onetwothreefour",
			hasNot: []string{"COMMENTX"},
		},
		{
			name:   "unterminated comment drops the rest",
			in:     `kept<!-- COMMENTX <p>never closed`,
			want:   "kept",
			hasNot: []string{"COMMENTX", "never"},
		},
		{
			name:   "processing instruction and declaration",
			in:     `<?xml version="1.0"?><!DOCTYPE html>text<![CDATA[x]]>`,
			want:   "text",
			hasNot: []string{"xml", "DOCTYPE"},
		},
		{
			name: "attribute with > inside quotes",
			in:   `<a title="a>b" data-x='c>d' href=x>link</a> done`,
			want: "link done",
		},
		{
			name: "apostrophe in attribute name is not a quote",
			in:   `<a don't>word</a>`,
			want: "word",
		},
		{
			name:    "less-than as text",
			in:      `a < b and 1<2 and x<`,
			want:    "a < b and 1<2 and x<",
			allowLT: true,
		},
		{
			name:    "escaped markup becomes text once",
			in:      `<p>&lt;b&gt;bold&lt;/b&gt; &lt;script&gt;</p>`,
			want:    "<b>bold</b> <script>",
			allowLT: true,
		},
		{
			name: "nbsp and numeric entities",
			in:   `a&nbsp;&nbsp;b &#65;&#x42;&#x263A; &amp;amp;`,
			want: "a b AB☺ &amp;",
		},
		{
			name: "block tags become newlines",
			in:   `<h1>Title</h1><ul><li>one</li><li>two</li></ul>x<br>y<br/>z<hr>w<span>in</span>line`,
			want: "Title\none\ntwo\nx\ny\nz\nwinline",
		},
		{
			name: "whitespace collapse",
			in:   "\n\n  <div>  a \t　 b  </div>\r\n\r\n\n\n<p>\tc  d </p>  \n\n  ",
			want: "a b\nc d",
		},
		{
			name: "invalid UTF-8",
			in:   "<p>a\xffb\xc3</p>",
			want: "a�b�",
		},
		{
			name: "unterminated tag drops the rest",
			in:   `text<a href="x`,
			want: "text",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := htmlToText(tc.in)
			if tc.want != "" || tc.in == "" {
				if got != tc.want {
					t.Errorf("htmlToText(%q) = %q, want %q", tc.in, got, tc.want)
				}
			}
			for _, h := range tc.has {
				if !strings.Contains(got, h) {
					t.Errorf("htmlToText(%q) = %q, want it to contain %q", tc.in, got, h)
				}
			}
			for _, h := range tc.hasNot {
				if strings.Contains(got, h) {
					t.Errorf("htmlToText(%q) = %q, must not contain %q", tc.in, got, h)
				}
			}
			if !tc.allowLT && strings.ContainsAny(got, "<>") {
				t.Errorf("htmlToText(%q) = %q, must not contain < or >", tc.in, got)
			}
			if got != strings.TrimSpace(got) {
				t.Errorf("htmlToText(%q) = %q, not trimmed", tc.in, got)
			}
		})
	}
}

func TestHTMLToTextLarge(t *testing.T) {
	const mib = 1 << 20
	inputs := []struct {
		name string
		in   string
	}{
		{"1 MiB of <", strings.Repeat("<", mib)},
		{"1 MiB of unterminated <a ", strings.Repeat("<a ", mib/3)},
		{"100000 nested div", strings.Repeat("<div>", 100000) + "x" + strings.Repeat("</div>", 100000)},
		{"1 MiB of unterminated quotes", strings.Repeat(`<a b="`, mib/6)},
		{"1 MiB of unclosed scripts", strings.Repeat("<script></scrip", mib/15)},
	}
	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			_ = htmlToText(tc.in)
			if d := time.Since(start); d >= 2*time.Second {
				t.Fatalf("htmlToText took %v, want < 2s", d)
			}
		})
	}
}

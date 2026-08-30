package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEndToEnd(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	output := filepath.Join(root, "public")
	mustWrite(t, filepath.Join(content, "@home", "index.md"), `---
title: "@home"
---
Главная страница.
`)
	mustWrite(t, filepath.Join(content, "blog", "first.md"), `---
title: Первый <пост>
date: 2025-01-03
tags: [go, web]
---
# Первый <пост>

Короткое **описание** первой публикации.

[Вторая](second.md)

![Картинка](image.png)

![Слева](left.png)
![Справа](right.png)

![Сверху](top.png)

![Снизу](bottom.png)

Строчная формула $a*b*c + x_i = \frac{1}{2}$ внутри текста.

$$
\sum_{i=1}^{n} x_i < y
$$

<script>alert("unsafe")</script>
`)
	mustWrite(t, filepath.Join(content, "blog", "second.md"), `---
title: Вторая публикация
date: 2025-02-04
---
Текст второй публикации.
`)
	mustWrite(t, filepath.Join(content, "blog", "image.png"), "not really an image")
	mustWrite(t, filepath.Join(content, "README.md"), "Repository documentation.\n")
	mustWrite(t, filepath.Join(content, ".gitignore"), "local files\n")
	mustWrite(t, filepath.Join(content, ".git", "config"), "private repository metadata\n")
	mustWrite(t, filepath.Join(content, "about", "index.md"), `---
title: Обо мне
date: 2024-01-01
---
Единственная страница раздела.
`)

	options := testBuildOptions(content, output)
	options.BaseURL = "https://blog.example"
	result, err := Build(options)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if result.Posts != 4 || result.Assets != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, font := range []string{
		"pt-serif-regular-latin.woff2",
		"pt-serif-regular-cyrillic.woff2",
		"pt-serif-bold-cyrillic.woff2",
		"source-code-pro-regular-latin.woff2",
	} {
		if _, err := os.Stat(filepath.Join(output, "assets", "fonts", font)); err != nil {
			t.Fatalf("bundled font %s was not published: %v", font, err)
		}
	}
	for _, asset := range []string{
		"katex.min.css",
		"katex.min.js",
		"auto-render.min.js",
		filepath.Join("fonts", "KaTeX_Main-Regular.woff2"),
		"LICENSE.txt",
	} {
		if _, err := os.Stat(filepath.Join(output, "assets", "katex", asset)); err != nil {
			t.Fatalf("KaTeX asset %s was not published: %v", asset, err)
		}
	}
	home := mustRead(t, filepath.Join(output, "index.html"))
	assertContains(t, home, `<html lang="en">`)
	assertContains(t, home, `<meta property="og:site_name" content="Test site">`)
	assertContains(t, home, `aria-label="Navigation"`)
	assertContains(t, home, `aria-label="Theme"`)
	assertContains(t, home, `<a href="/" aria-current="page">@home</a>`)
	assertContains(t, home, `<h1>@home</h1>`)
	assertContains(t, home, `Главная страница.`)

	first := mustRead(t, filepath.Join(output, "blog", "first", "index.html"))
	assertContains(t, first, `<a href="/blog/second/">Вторая</a>`)
	assertContains(t, first, `<img src="/blog/image.png" alt="Картинка">`)
	assertContains(t, first, `<figure><img src="/blog/image.png" alt="Картинка"><figcaption>Картинка</figcaption></figure>`)
	assertContains(t, first, `<div class="image-pair"><figure><img src="/blog/left.png" alt="Слева"><figcaption>Слева</figcaption></figure><figure><img src="/blog/right.png" alt="Справа"><figcaption>Справа</figcaption></figure></div>`)
	assertContains(t, first, `<figure><img src="/blog/top.png" alt="Сверху"><figcaption>Сверху</figcaption></figure>`)
	assertContains(t, first, `<figure><img src="/blog/bottom.png" alt="Снизу"><figcaption>Снизу</figcaption></figure>`)
	assertContains(t, first, `<span class="math-inline">\(a*b*c + x_i = \frac{1}{2}\)</span>`)
	assertContains(t, first, `<div class="math-display">\[\sum_{i=1}^{n} x_i &lt; y`)
	if strings.Contains(first, `<em>b</em>`) {
		t.Fatal("Markdown emphasis was rendered inside a formula")
	}
	assertContains(t, first, `<link rel="stylesheet" href="/assets/katex/katex.min.css">`)
	assertContains(t, first, `<script src="/assets/katex/katex.min.js" defer></script>`)
	assertContains(t, first, `<time datetime="2025-01-03">3 January 2025</time>`)
	assertContains(t, first, `<!-- raw HTML omitted -->`)
	if strings.Count(first, "<h1>") != 1 {
		t.Fatalf("duplicate article title in output:\n%s", first)
	}
	assertContains(t, first, `Короткое описание первой публикации.`)

	section := mustRead(t, filepath.Join(output, "blog", "index.html"))
	if strings.Index(section, "Вторая публикация") > strings.Index(section, "Первый &lt;пост&gt;") {
		t.Fatal("posts are not ordered newest first")
	}
	assertContains(t, section, `<ul class="article-list">`)
	assertContains(t, section, `<time datetime="2025-02-04">4 February 2025</time>`)
	about := mustRead(t, filepath.Join(output, "about", "index.html"))
	assertContains(t, about, "Единственная страница раздела.")
	assertContains(t, about, `<a href="/about/" aria-current="page">about</a>`)
	if _, err := os.Stat(filepath.Join(output, "about", "index", "index.html")); !os.IsNotExist(err) {
		t.Fatal("single-post section created a redundant nested article page")
	}
	assertContains(t, mustRead(t, filepath.Join(output, "sitemap.xml")), "https://blog.example/blog/second/")
	assertContains(t, mustRead(t, filepath.Join(output, "sitemap.xml")), "2025-02-04")
	assertContains(t, mustRead(t, filepath.Join(output, "robots.txt")), "https://blog.example/sitemap.xml")
	assertContains(t, mustRead(t, filepath.Join(output, "404.html")), `>Test site</a>`)
	if _, err := os.Stat(filepath.Join(output, "feed.xml")); !os.IsNotExist(err) {
		t.Fatal("RSS output must not be generated")
	}
	for _, hidden := range []string{".git", ".gitignore", "README.html", "README.md"} {
		if _, err := os.Stat(filepath.Join(output, hidden)); !os.IsNotExist(err) {
			t.Fatalf("repository metadata was published: %s", hidden)
		}
	}
}

func TestBuildValidatesPathsAndMetadata(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	mustWrite(t, filepath.Join(content, "blog", "bad.md"), `---
title: Bad
date: tomorrow
---
Text.
`)
	if _, err := Build(testBuildOptions(content, filepath.Join(content, "dist"))); err == nil {
		t.Fatal("expected unsafe output path error")
	}
	if _, err := Build(testBuildOptions(content, filepath.Join(root, "dist"))); err == nil || !strings.Contains(err.Error(), "expected YYYY-MM-DD") {
		t.Fatalf("expected date validation error, got %v", err)
	}
}

func TestSplitFrontMatterWithoutHeader(t *testing.T) {
	fm, body, err := splitFrontMatter([]byte("# Plain markdown\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "" || string(body) != "# Plain markdown\n" {
		t.Fatalf("unexpected split: %+v %q", fm, body)
	}
}

func mustWrite(t *testing.T, name, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testBuildOptions(content, output string) Options {
	return Options{
		ContentDir: content, OutputDir: output, SiteTitle: "Test site", Language: "en",
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output does not contain %q:\n%s", want, got)
	}
}

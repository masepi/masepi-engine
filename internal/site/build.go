package site

import (
	"bytes"
	"embed"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed theme/*
var themeFS embed.FS

type builder struct {
	options  Options
	config   Config
	markdown *markdownRenderer
	assets   int
}

func Build(options Options) (BuildResult, error) {
	b, err := newBuilder(options)
	if err != nil {
		return BuildResult{}, err
	}
	return b.build()
}

func newBuilder(options Options) (*builder, error) {
	if options.ContentDir == "" {
		options.ContentDir = "content"
	}
	if options.OutputDir == "" {
		options.OutputDir = "dist"
	}
	content, err := filepath.Abs(options.ContentDir)
	if err != nil {
		return nil, err
	}
	output, err := filepath.Abs(options.OutputDir)
	if err != nil {
		return nil, err
	}
	if content == output || strings.HasPrefix(output+string(filepath.Separator), content+string(filepath.Separator)) {
		return nil, errors.New("output directory must not be the content directory or one of its children")
	}
	if output == filepath.VolumeName(output)+string(filepath.Separator) {
		return nil, errors.New("refusing to use a filesystem root as output directory")
	}
	info, err := os.Stat(content)
	if err != nil {
		return nil, fmt.Errorf("content directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("content path %s is not a directory", content)
	}
	options.ContentDir, options.OutputDir = content, output

	config := Config{
		Title: strings.TrimSpace(options.SiteTitle), Language: strings.TrimSpace(options.Language),
	}
	if options.BaseURL != "" {
		config.BaseURL = options.BaseURL
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	if config.BaseURL != "" {
		parsed, err := url.ParseRequestURI(config.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("base URL must be an absolute http(s) URL, got %q", config.BaseURL)
		}
	}
	if config.Title == "" || config.Language == "" {
		return nil, errors.New("site title and language must not be empty")
	}
	return &builder{options: options, config: config, markdown: newMarkdownRenderer()}, nil
}

func (b *builder) build() (BuildResult, error) {
	sections, posts, err := b.loadSections()
	if err != nil {
		return BuildResult{}, err
	}
	if len(sections) == 0 {
		return BuildResult{}, errors.New("content does not contain any sections with published Markdown files")
	}
	sections[0].URL = "/"
	for _, section := range sections {
		if len(section.Posts) == 1 {
			section.Posts[0].URL = section.URL
		} else if section.URL == "/" {
			for _, post := range section.Posts {
				post.URL = "/" + post.Slug + "/"
			}
		}
	}
	if err := validateFinalURLs(posts); err != nil {
		return BuildResult{}, err
	}

	parent := filepath.Dir(b.options.OutputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return BuildResult{}, err
	}
	staging, err := os.MkdirTemp(parent, ".masepi-build-")
	if err != nil {
		return BuildResult{}, err
	}
	defer os.RemoveAll(staging)

	if err := b.copyAssets(staging); err != nil {
		return BuildResult{}, err
	}
	if err := b.writeTheme(staging); err != nil {
		return BuildResult{}, err
	}
	if err := b.writePages(staging, sections, posts); err != nil {
		return BuildResult{}, err
	}
	if err := b.replaceOutput(staging); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{Posts: len(posts), Assets: b.assets, Output: b.options.OutputDir}, nil
}

func (b *builder) loadSections() ([]*Section, []*Post, error) {
	entries, err := os.ReadDir(b.options.ContentDir)
	if err != nil {
		return nil, nil, err
	}
	var sections []*Section
	var posts []*Post
	seen := make(map[string]string)
	for _, rootEntry := range entries {
		if !rootEntry.IsDir() || strings.HasPrefix(rootEntry.Name(), ".") {
			continue
		}
		section := &Section{Name: strings.ReplaceAll(rootEntry.Name(), "_", " "), Slug: rootEntry.Name(), URL: "/" + rootEntry.Name() + "/"}
		sectionDir := filepath.Join(b.options.ContentDir, rootEntry.Name())
		err := filepath.WalkDir(sectionDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if filePath != sectionDir && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic links are not supported: %s", filePath)
			}
			rel, err := filepath.Rel(b.options.ContentDir, filePath)
			if err != nil {
				return err
			}
			post, err := b.loadPost(filePath, filepath.ToSlash(rel), section)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if post == nil {
				return nil
			}
			if other, exists := seen[post.URL]; exists {
				return fmt.Errorf("posts %s and %s resolve to the same URL %s", other, rel, post.URL)
			}
			seen[post.URL] = rel
			section.Posts = append(section.Posts, post)
			posts = append(posts, post)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		if len(section.Posts) == 0 {
			continue
		}
		sortPosts(section.Posts)
		sections = append(sections, section)
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Name < sections[j].Name })
	sortPosts(posts)
	return sections, posts, nil
}

func validateFinalURLs(posts []*Post) error {
	seen := make(map[string]string)
	for _, post := range posts {
		if other, exists := seen[post.URL]; exists {
			return fmt.Errorf("posts %s and %s resolve to the same URL %s", other, post.SourceRel, post.URL)
		}
		seen[post.URL] = post.SourceRel
	}
	return nil
}

func sortPosts(posts []*Post) {
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].Date.Equal(posts[j].Date) {
			return posts[i].URL < posts[j].URL
		}
		return posts[i].Date.After(posts[j].Date)
	})
}

func (b *builder) loadPost(filePath, rel string, section *Section) (*Post, error) {
	input, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	fm, body, err := splitFrontMatter(input)
	if err != nil {
		return nil, err
	}
	if fm.Title == "" {
		return nil, errors.New("title is required")
	}
	var date time.Time
	if fm.Date != "" {
		date, err = parseDate(fm.Date)
		if err != nil {
			return nil, err
		}
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(rel), section.Slug+"/"), path.Ext(rel))
	slug = strings.Trim(slug, "/")
	if !validSlug(slug) {
		return nil, fmt.Errorf("invalid slug %q", slug)
	}
	postURL := section.URL + slug + "/"
	html, autoDescription, err := b.markdown.render(body, rel, fm.Title)
	if err != nil {
		return nil, err
	}
	return &Post{
		Title: fm.Title, Description: autoDescription, Date: date, Slug: slug,
		URL: postURL, SourceRel: rel, Tags: fm.Tags, Body: html, Section: section.Slug,
	}, nil
}

func (b *builder) copyAssets(staging string) error {
	return filepath.WalkDir(b.options.ContentDir, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if source != b.options.ContentDir && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", source)
		}
		name := entry.Name()
		if strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		rel, err := filepath.Rel(b.options.ContentDir, source)
		if err != nil {
			return err
		}
		target := filepath.Join(staging, rel)
		if err := copyFile(source, target); err != nil {
			return fmt.Errorf("copy asset %s: %w", rel, err)
		}
		b.assets++
		return nil
	})
}

func validSlug(slug string) bool {
	if slug == "" || slug == "." || path.Clean(slug) != slug || strings.ContainsAny(slug, "\\?#") {
		return false
	}
	for _, segment := range strings.Split(slug, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, char := range segment {
			if char < 0x20 {
				return false
			}
		}
	}
	return true
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (b *builder) writeTheme(staging string) error {
	for _, name := range []string{"site.css", "site.js"} {
		data, err := themeFS.ReadFile("theme/" + name)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(staging, "assets", name), data); err != nil {
			return err
		}
	}
	for _, directory := range []string{"theme/fonts", "theme/katex"} {
		if err := fs.WalkDir(themeFS, directory, func(source string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, err := themeFS.ReadFile(source)
			if err != nil {
				return err
			}
			relative := strings.TrimPrefix(source, "theme/")
			return writeFile(filepath.Join(staging, "assets", filepath.FromSlash(relative)), data)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) writePages(staging string, sections []*Section, posts []*Post) error {
	base := pageData{Site: b.config, Sections: sections}
	for _, section := range sections {
		if len(section.Posts) == 1 {
			if err := b.writePostPage(staging, base, section, section.Posts[0]); err != nil {
				return err
			}
			continue
		}
		data := base
		data.PageTitle = section.Name
		data.Description = section.Name
		data.Canonical = b.absoluteURL(section.URL)
		data.Section = section
		data.Posts = section.Posts
		if err := b.renderTemplate(staging, path.Join(strings.TrimPrefix(section.URL, "/"), "index.html"), "section.html", data); err != nil {
			return err
		}
		for _, post := range section.Posts {
			if err := b.writePostPage(staging, base, section, post); err != nil {
				return err
			}
		}
	}
	notFound := base
	notFound.PageTitle = "404 — " + b.config.Title
	if err := b.renderTemplate(staging, "404.html", "404.html", notFound); err != nil {
		return err
	}
	if err := b.writeSitemap(staging, posts); err != nil {
		return err
	}
	robots := "User-agent: *\nAllow: /\n"
	if b.config.BaseURL != "" {
		robots += "Sitemap: " + b.absoluteURL("/sitemap.xml") + "\n"
	}
	return writeFile(filepath.Join(staging, "robots.txt"), []byte(robots))
}

func (b *builder) writePostPage(staging string, base pageData, section *Section, post *Post) error {
	data := base
	data.PageTitle = post.Title
	data.Description = post.Description
	data.Canonical = b.absoluteURL(post.URL)
	data.Post = post
	data.Section = section
	return b.renderTemplate(staging, path.Join(strings.TrimPrefix(post.URL, "/"), "index.html"), "post.html", data)
}

func (b *builder) renderTemplate(staging, target, page string, data pageData) error {
	tmpl, err := template.New("base.html").ParseFS(themeFS, "theme/base.html", "theme/"+page)
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "base", data); err != nil {
		return err
	}
	return writeFile(filepath.Join(staging, filepath.FromSlash(target)), rendered.Bytes())
}

func (b *builder) replaceOutput(staging string) error {
	if err := os.RemoveAll(b.options.OutputDir); err != nil {
		return fmt.Errorf("clear output directory: %w", err)
	}
	if err := os.Rename(staging, b.options.OutputDir); err != nil {
		return fmt.Errorf("publish output directory: %w", err)
	}
	return nil
}

func writeFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func (b *builder) absoluteURL(relative string) string {
	if b.config.BaseURL == "" {
		return relative
	}
	return b.config.BaseURL + "/" + strings.TrimLeft(relative, "/")
}

type sitemap struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location string `xml:"loc"`
	Modified string `xml:"lastmod,omitempty"`
}

func (b *builder) writeSitemap(staging string, posts []*Post) error {
	set := sitemap{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	set.URLs = append(set.URLs, sitemapURL{Location: b.absoluteURL("/")})
	for _, post := range posts {
		if post.URL == "/" {
			continue
		}
		modified := ""
		if !post.Date.IsZero() {
			modified = post.Date.Format("2006-01-02")
		}
		set.URLs = append(set.URLs, sitemapURL{Location: b.absoluteURL(post.URL), Modified: modified})
	}
	data, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return writeFile(filepath.Join(staging, "sitemap.xml"), data)
}

package site

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const manifestVersion = 1
const manifestName = ".masepi-manifest.json"
const releasesToKeep = 3

type PublishOptions struct {
	ContentDir     string
	SiteRoot       string
	BaseURL        string
	SiteTitle      string
	Language       string
	ContentVersion string
	EngineVersion  string
}

type PublishResult struct {
	BuildResult
	Full     bool
	Changed  int
	Release  string
	Revision string
}

type buildManifest struct {
	Version        int                     `json:"version"`
	ContentVersion string                  `json:"content_version"`
	EngineVersion  string                  `json:"engine_version"`
	ConfigHash     string                  `json:"config_hash"`
	ThemeHash      string                  `json:"theme_hash"`
	Files          map[string]manifestFile `json:"files"`
	Posts          map[string]manifestPost `json:"posts"`
	Sections       []manifestSection       `json:"sections"`
}

type manifestFile struct {
	Hash string `json:"hash"`
	Kind string `json:"kind"`
}

type manifestPost struct {
	Published   bool     `json:"published"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Date        string   `json:"date,omitempty"`
	Slug        string   `json:"slug,omitempty"`
	URL         string   `json:"url,omitempty"`
	SourceRel   string   `json:"source_rel"`
	Tags        []string `json:"tags,omitempty"`
	Body        string   `json:"body,omitempty"`
	Section     string   `json:"section,omitempty"`
}

type manifestSection struct {
	Name  string   `json:"name"`
	Slug  string   `json:"slug"`
	URL   string   `json:"url"`
	Posts []string `json:"posts"`
}

type buildSnapshot struct {
	manifest *buildManifest
	sections []*Section
	posts    []*Post
	changed  map[string]bool
}

func Publish(options PublishOptions) (PublishResult, error) {
	if strings.TrimSpace(options.SiteRoot) == "" {
		return PublishResult{}, errors.New("site root is required")
	}
	b, err := newBuilder(Options{
		ContentDir: options.ContentDir, BaseURL: options.BaseURL, SiteTitle: options.SiteTitle, Language: options.Language,
	})
	if err != nil {
		return PublishResult{}, err
	}

	current, previous, err := currentRelease(options.SiteRoot)
	if err != nil {
		return PublishResult{}, err
	}
	snapshot, err := b.snapshot(previous, options)
	if err != nil {
		return PublishResult{}, err
	}
	full := previous == nil || previous.Version != manifestVersion ||
		previous.EngineVersion != snapshot.manifest.EngineVersion ||
		previous.ConfigHash != snapshot.manifest.ConfigHash ||
		previous.ThemeHash != snapshot.manifest.ThemeHash ||
		!sameSectionSet(previous.Sections, snapshot.manifest.Sections)
	if full && previous != nil && (previous.Version != manifestVersion || previous.EngineVersion != snapshot.manifest.EngineVersion || previous.ThemeHash != snapshot.manifest.ThemeHash) {
		snapshot, err = b.snapshot(nil, options)
		if err != nil {
			return PublishResult{}, err
		}
	}

	if previous != nil && previous.ContentVersion == options.ContentVersion && !full && len(snapshot.changed) == 0 {
		return PublishResult{BuildResult: BuildResult{Posts: len(snapshot.posts), Output: current}, Revision: options.ContentVersion}, nil
	}

	releasesDir := filepath.Join(options.SiteRoot, "releases")
	if err := os.MkdirAll(releasesDir, 0o755); err != nil {
		return PublishResult{}, err
	}
	releaseName := releaseName(options.ContentVersion, options.EngineVersion)
	temporary := filepath.Join(releasesDir, "."+releaseName+".tmp")
	final := filepath.Join(releasesDir, releaseName)
	if err := os.Mkdir(temporary, 0o755); err != nil {
		return PublishResult{}, err
	}
	defer os.RemoveAll(temporary)

	assets := 0
	if full {
		result, buildErr := Build(Options{
			ContentDir: options.ContentDir, OutputDir: temporary, BaseURL: options.BaseURL,
			SiteTitle: options.SiteTitle, Language: options.Language,
		})
		if buildErr != nil {
			return PublishResult{}, buildErr
		}
		assets = result.Assets
	} else {
		if err := cloneRelease(current, temporary); err != nil {
			return PublishResult{}, fmt.Errorf("clone current release: %w", err)
		}
		var incrementalErr error
		assets, incrementalErr = b.applyIncremental(temporary, previous, snapshot)
		if incrementalErr != nil {
			return PublishResult{}, incrementalErr
		}
	}
	if err := writeManifest(temporary, snapshot.manifest); err != nil {
		return PublishResult{}, err
	}
	if err := os.Rename(temporary, final); err != nil {
		return PublishResult{}, fmt.Errorf("finish release: %w", err)
	}
	if err := switchCurrent(options.SiteRoot, releaseName); err != nil {
		return PublishResult{}, err
	}
	if err := pruneReleases(options.SiteRoot, releaseName, releasesToKeep); err != nil {
		log.Printf("не удалось удалить старые релизы: %v", err)
	}
	return PublishResult{
		BuildResult: BuildResult{Posts: len(snapshot.posts), Assets: assets, Output: final},
		Full:        full, Changed: len(snapshot.changed), Release: releaseName, Revision: options.ContentVersion,
	}, nil
}

func (b *builder) snapshot(previous *buildManifest, options PublishOptions) (*buildSnapshot, error) {
	configData, _ := json.Marshal(b.config)
	themeHash, err := embeddedThemeHash()
	if err != nil {
		return nil, err
	}
	manifest := &buildManifest{
		Version: manifestVersion, ContentVersion: options.ContentVersion, EngineVersion: options.EngineVersion,
		ConfigHash: bytesHash(configData), ThemeHash: themeHash,
		Files: make(map[string]manifestFile), Posts: make(map[string]manifestPost),
	}
	changed := make(map[string]bool)
	sectionMap := make(map[string]*Section)

	err = filepath.WalkDir(b.options.ContentDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath != b.options.ContentDir && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", filePath)
		}
		rel, err := filepath.Rel(b.options.ContentDir, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/") && strings.EqualFold(path.Ext(rel), ".md") {
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		kind := "asset"
		if strings.EqualFold(path.Ext(rel), ".md") {
			kind = "markdown"
		}
		state := manifestFile{Hash: bytesHash(data), Kind: kind}
		manifest.Files[rel] = state
		oldFile, unchanged := manifestFile{}, false
		if previous != nil {
			oldFile, unchanged = previous.Files[rel]
			unchanged = unchanged && oldFile == state
		}
		if !unchanged {
			changed[rel] = true
		}
		if kind != "markdown" {
			return nil
		}

		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return fmt.Errorf("Markdown file must belong to a section: %s", rel)
		}
		sectionSlug := parts[0]
		section := sectionMap[sectionSlug]
		if section == nil {
			section = &Section{Name: strings.ReplaceAll(sectionSlug, "_", " "), Slug: sectionSlug, URL: "/" + sectionSlug + "/"}
			sectionMap[sectionSlug] = section
		}

		var postValue *Post
		if unchanged {
			if old, ok := previous.Posts[rel]; ok {
				postValue, err = postFromManifest(old, section)
			} else {
				unchanged = false
				changed[rel] = true
			}
		}
		if !unchanged {
			postValue, err = b.loadPost(filePath, rel, section)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if postValue != nil {
			section.Posts = append(section.Posts, postValue)
		}
		manifest.Posts[rel] = manifestPostFromPost(rel, postValue)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if previous != nil {
		for rel := range previous.Files {
			if _, exists := manifest.Files[rel]; !exists {
				changed[rel] = true
			}
		}
	}

	sections := make([]*Section, 0, len(sectionMap))
	for _, section := range sectionMap {
		if len(section.Posts) == 0 {
			continue
		}
		sortPosts(section.Posts)
		sections = append(sections, section)
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Name < sections[j].Name })
	if len(sections) == 0 {
		return nil, errors.New("content does not contain any sections with published Markdown files")
	}
	sections[0].URL = "/"
	var posts []*Post
	for _, section := range sections {
		if len(section.Posts) == 1 {
			section.Posts[0].URL = section.URL
		} else if section.URL == "/" {
			for _, postValue := range section.Posts {
				postValue.URL = "/" + postValue.Slug + "/"
			}
		}
		posts = append(posts, section.Posts...)
	}
	if err := validateFinalURLs(posts); err != nil {
		return nil, err
	}
	sortPosts(posts)

	for _, section := range sections {
		stored := manifestSection{Name: section.Name, Slug: section.Slug, URL: section.URL}
		for _, postValue := range section.Posts {
			stored.Posts = append(stored.Posts, postValue.SourceRel)
			manifest.Posts[postValue.SourceRel] = manifestPostFromPost(postValue.SourceRel, postValue)
		}
		manifest.Sections = append(manifest.Sections, stored)
	}
	return &buildSnapshot{manifest: manifest, sections: sections, posts: posts, changed: changed}, nil
}

func (b *builder) applyIncremental(target string, previous *buildManifest, snapshot *buildSnapshot) (int, error) {
	base := pageData{Site: b.config, AssetVersion: b.assetVersion, Sections: snapshot.sections}
	newSections := sectionBySlug(snapshot.sections)
	oldSections := manifestSectionBySlug(previous.Sections)
	rebuildWholeSection := make(map[string]bool)
	rebuildIndex := make(map[string]bool)
	sitemapChanged := false
	for slug, section := range newSections {
		if sectionCardinalityClass(oldSections[slug]) != sectionCardinalityClass(manifestSectionFor(section)) {
			rebuildWholeSection[slug] = true
		}
	}

	for rel := range snapshot.changed {
		newFile, newExists := snapshot.manifest.Files[rel]
		oldFile, oldExists := previous.Files[rel]
		kind := newFile.Kind
		if !newExists {
			kind = oldFile.Kind
		}
		if kind == "asset" {
			targetPath := filepath.Join(target, filepath.FromSlash(rel))
			if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return 0, err
			}
			if newExists {
				if err := copyFile(filepath.Join(b.options.ContentDir, filepath.FromSlash(rel)), targetPath); err != nil {
					return 0, err
				}
			}
			continue
		}
		if kind != "markdown" {
			continue
		}

		oldPost, hadOld := previous.Posts[rel]
		newPost, hasNew := snapshot.manifest.Posts[rel]
		oldSection := oldPost.Section
		newSection := newPost.Section
		listingChanged := !listingPostEqual(oldPost, newPost) || oldExists != newExists
		if listingChanged && oldSection != "" {
			rebuildIndex[oldSection] = true
		}
		if listingChanged && newSection != "" {
			rebuildIndex[newSection] = true
		}
		if hadOld && oldPost.Published {
			if err := removeURL(target, oldPost.URL); err != nil {
				return 0, err
			}
		}
		if hasNew && newPost.Published && !rebuildWholeSection[newSection] {
			postValue := findPost(snapshot.posts, rel)
			if postValue == nil {
				return 0, fmt.Errorf("changed post %s is missing from snapshot", rel)
			}
			if err := b.writePostPage(target, base, newSections[newSection], postValue); err != nil {
				return 0, err
			}
		}
		if listingChanged {
			sitemapChanged = true
		}
	}

	for slug := range rebuildWholeSection {
		if old, ok := oldSections[slug]; ok {
			if err := removeManifestSectionOutputs(target, old, previous.Posts); err != nil {
				return 0, err
			}
		}
		section := newSections[slug]
		if section != nil {
			if err := b.writeSection(target, base, section); err != nil {
				return 0, err
			}
		}
		delete(rebuildIndex, slug)
		sitemapChanged = true
	}
	for slug := range rebuildIndex {
		section := newSections[slug]
		if section == nil || len(section.Posts) == 1 {
			continue
		}
		data := base
		data.PageTitle = section.Name
		data.Description = section.Name
		data.Canonical = b.absoluteURL(section.URL)
		data.Section = section
		data.Posts = section.Posts
		if err := b.renderTemplate(target, path.Join(strings.TrimPrefix(section.URL, "/"), "index.html"), "section.html", data); err != nil {
			return 0, err
		}
	}
	if sitemapChanged {
		if err := b.writeSitemap(target, snapshot.posts); err != nil {
			return 0, err
		}
	}
	return countAssets(snapshot.manifest.Files), nil
}

func (b *builder) writeSection(target string, base pageData, section *Section) error {
	if len(section.Posts) == 1 {
		return b.writePostPage(target, base, section, section.Posts[0])
	}
	data := base
	data.PageTitle, data.Description, data.Canonical = section.Name, section.Name, b.absoluteURL(section.URL)
	data.Section, data.Posts = section, section.Posts
	if err := b.renderTemplate(target, path.Join(strings.TrimPrefix(section.URL, "/"), "index.html"), "section.html", data); err != nil {
		return err
	}
	for _, postValue := range section.Posts {
		if err := b.writePostPage(target, base, section, postValue); err != nil {
			return err
		}
	}
	return nil
}

func currentRelease(siteRoot string) (string, *buildManifest, error) {
	current := filepath.Join(siteRoot, "current")
	resolved, err := filepath.EvalSymlinks(current)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("resolve current release: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(resolved, manifestName))
	if err != nil {
		return resolved, nil, nil
	}
	var manifest buildManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return resolved, nil, nil
	}
	return resolved, &manifest, nil
}

func cloneRelease(source, target string) error {
	if source == "" {
		return errors.New("current release does not exist")
	}
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, name)
		if err != nil || rel == "." {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in release: %s", name)
		}
		return os.Link(name, destination)
	})
}

func writeManifest(directory string, manifest *buildManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(directory, manifestName)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeFile(target, append(data, '\n'))
}

func switchCurrent(siteRoot, release string) error {
	temporary := filepath.Join(siteRoot, fmt.Sprintf(".current-%d", time.Now().UnixNano()))
	if err := os.Symlink(filepath.Join("releases", release), temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(siteRoot, "current")); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("switch current release: %w", err)
	}
	return nil
}

func pruneReleases(siteRoot, current string, keep int) error {
	directory := filepath.Join(siteRoot, "releases")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type releaseEntry struct {
		name     string
		modified time.Time
	}
	var releases []releaseEntry
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		releases = append(releases, releaseEntry{entry.Name(), info.ModTime()})
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].modified.After(releases[j].modified) })
	kept := 0
	for _, release := range releases {
		if release.name == current || kept < keep {
			kept++
			continue
		}
		if err := os.RemoveAll(filepath.Join(directory, release.name)); err != nil {
			return err
		}
	}
	return nil
}

func embeddedThemeHash() (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(themeFS, "theme", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := themeFS.ReadFile(name)
		if err != nil {
			return err
		}
		hash.Write([]byte(name))
		hash.Write(data)
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}

func bytesHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func releaseName(content, engine string) string {
	short := func(value string) string {
		value = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return -1
		}, value)
		if len(value) > 12 {
			value = value[:12]
		}
		if value == "" {
			value = "unknown"
		}
		return value
	}
	return fmt.Sprintf("%s-%s-%d", short(content), short(engine), time.Now().UnixNano())
}

func sameSectionSet(old []manifestSection, current []manifestSection) bool {
	if len(old) != len(current) {
		return false
	}
	for i := range old {
		if old[i].Slug != current[i].Slug || old[i].URL != current[i].URL {
			return false
		}
	}
	return true
}

func postFromManifest(stored manifestPost, section *Section) (*Post, error) {
	if !stored.Published {
		return nil, nil
	}
	var date time.Time
	var err error
	if stored.Date != "" {
		date, err = time.Parse(time.RFC3339Nano, stored.Date)
	}
	if err != nil {
		return nil, err
	}
	return &Post{Title: stored.Title, Description: stored.Description, Date: date, Slug: stored.Slug,
		URL: section.URL + stored.Slug + "/", SourceRel: stored.SourceRel, Tags: stored.Tags,
		Body: template.HTML(stored.Body), Section: section.Slug}, nil
}

func manifestPostFromPost(rel string, postValue *Post) manifestPost {
	if postValue == nil {
		return manifestPost{SourceRel: rel}
	}
	date := ""
	if !postValue.Date.IsZero() {
		date = postValue.Date.Format(time.RFC3339Nano)
	}
	return manifestPost{Published: true, Title: postValue.Title, Description: postValue.Description,
		Date: date, Slug: postValue.Slug, URL: postValue.URL, SourceRel: rel, Tags: postValue.Tags,
		Body: string(postValue.Body), Section: postValue.Section}
}

func sectionBySlug(sections []*Section) map[string]*Section {
	result := make(map[string]*Section, len(sections))
	for _, section := range sections {
		result[section.Slug] = section
	}
	return result
}

func manifestSectionBySlug(sections []manifestSection) map[string]manifestSection {
	result := make(map[string]manifestSection, len(sections))
	for _, section := range sections {
		result[section.Slug] = section
	}
	return result
}

func manifestSectionFor(section *Section) manifestSection {
	if section == nil {
		return manifestSection{}
	}
	result := manifestSection{Slug: section.Slug}
	for _, postValue := range section.Posts {
		result.Posts = append(result.Posts, postValue.SourceRel)
	}
	return result
}

func sectionCardinalityClass(section manifestSection) int {
	if len(section.Posts) == 1 {
		return 1
	}
	if len(section.Posts) > 1 {
		return 2
	}
	return 0
}

func listingPostEqual(a, b manifestPost) bool {
	return a.Published == b.Published && a.Title == b.Title && a.Date == b.Date && a.URL == b.URL && a.Section == b.Section
}

func findPost(posts []*Post, rel string) *Post {
	for _, postValue := range posts {
		if postValue.SourceRel == rel {
			return postValue
		}
	}
	return nil
}

func removeURL(root, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	target := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(rawURL, "/")), "index.html")
	if rawURL == "/" {
		target = filepath.Join(root, "index.html")
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for directory := filepath.Dir(target); directory != root && strings.HasPrefix(directory, root+string(filepath.Separator)); directory = filepath.Dir(directory) {
		if err := os.Remove(directory); err != nil {
			break
		}
	}
	return nil
}

func removeManifestSectionOutputs(root string, section manifestSection, posts map[string]manifestPost) error {
	if err := removeURL(root, section.URL); err != nil {
		return err
	}
	for _, rel := range section.Posts {
		if err := removeURL(root, posts[rel].URL); err != nil {
			return err
		}
	}
	return nil
}

func countAssets(files map[string]manifestFile) int {
	count := 0
	for _, file := range files {
		if file.Kind == "asset" {
			count++
		}
	}
	return count
}

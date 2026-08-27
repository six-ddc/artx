// Package vault is the vault façade: locating vaults, scanning artifacts,
// assigning ids, and assembling api DTOs.
//
// Owned by W-core. Both CLI commands and HTTP handlers go through it, which
// keeps art list and GET /api/docs returning field-for-field identical
// content.
package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/six-ddc/art/internal/anchor"
	"github.com/six-ddc/art/internal/api"
	"github.com/six-ddc/art/internal/config"
	"github.com/six-ddc/art/internal/eventlog"
	"github.com/six-ddc/art/internal/gitx"
	"github.com/six-ddc/art/internal/idgen"
	"github.com/six-ddc/art/internal/mdsrc"
)

// Directory and file name constants.
const (
	ArtDir     = ".art"
	AssetsDir  = ".art/assets"
	AgentsFile = "AGENTS.md"
	IndexMD    = "index.md"
	IndexHTML  = "index.html"
)

// FrontmatterAIDKey is the frontmatter key that carries a document's id in md files.
const FrontmatterAIDKey = "aid"

// MetaAIDName is the <meta name> value that carries a document's id in html files.
const MetaAIDName = "aid"

// ErrNotFound indicates no artifact could be found by the given slug/id; maps to CLI exit code 2.
var ErrNotFound = errors.New("vault: artifact not found")

// ErrExists indicates the slug is already taken.
var ErrExists = errors.New("vault: slug already exists")

// ErrOutsideVault indicates a path escaped the vault directory.
var ErrOutsideVault = errors.New("vault: path escapes vault root")

// Vault is a located vault.
type Vault struct {
	Root  string
	Name  string
	Cfg   *config.Vault
	Store *eventlog.Store
	Git   *gitx.Repo
}

// Open opens the vault at root and loads its config. Returns an error if root is not a vault.
func Open(root, name string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, statErr := os.Stat(filepath.Join(abs, ArtDir))
	if statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("vault: %s is not a vault (missing %s): %w", abs, ArtDir, ErrNotFound)
	}
	cfg, err := config.LoadVault(abs)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	return &Vault{
		Root:  abs,
		Name:  name,
		Cfg:   cfg,
		Store: eventlog.Open(abs),
		Git:   gitx.Open(abs),
	}, nil
}

// Discover locates and opens a vault, following config.Resolve's priority order.
func Discover(explicit string) (*Vault, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, name, err := config.Resolve(explicit, cwd)
	if err != nil {
		if errors.Is(err, config.ErrNoVault) {
			return nil, fmt.Errorf("vault: no vault found: %w", ErrNotFound)
		}
		return nil, err
	}
	return Open(root, name)
}

// Init sets up a new vault at dir: directory skeleton, .art/config.yaml,
// .gitattributes, the AGENTS.md template, git init, and a global registry
// entry. Idempotent if dir is already a vault.
func Init(ctx context.Context, dir, name string) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, eventlog.CommentsDir), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, AssetsDir), 0o755); err != nil {
		return nil, err
	}

	if name == "" {
		name = filepath.Base(abs)
	}

	cfgPath := filepath.Join(abs, config.VaultConfigPath)
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		if err := config.SaveVault(abs, &config.Vault{Name: name}); err != nil {
			return nil, err
		}
	}

	// git is optional: art's core functionality never depends on it, so a
	// missing/unavailable git degrades silently here.
	_ = gitx.Open(abs).Init(ctx)

	if err := gitx.EnsureGitattributes(abs); err != nil {
		return nil, err
	}
	if err := gitx.EnsureGitignore(abs); err != nil {
		return nil, err
	}

	agentsPath := filepath.Join(abs, AgentsFile)
	if _, statErr := os.Stat(agentsPath); os.IsNotExist(statErr) {
		if err := os.WriteFile(agentsPath, AgentsTemplate(), 0o644); err != nil {
			return nil, err
		}
	}

	if err := config.Register(name, abs); err != nil {
		return nil, err
	}

	// Commit the skeleton we just created.
	//
	// .gitattributes in particular sets merge=union on .art/comments/*.yaml,
	// which is the entire mechanism behind "remote = git" multi-machine
	// convergence — if it never lands in the repo, that convergence simply
	// doesn't happen. The watcher's auto-commit only covers the artifact
	// directories plus .art/comments, so it never picks up these root-level
	// files; init has to commit them itself.
	if repo := gitx.Open(abs); repo.Available() {
		_, _ = repo.Commit(ctx, gitx.CommitOptions{
			Message: "art: init vault " + name,
			Author:  gitx.AuthorArt,
			Paths:   []string{".gitattributes", ".gitignore", AgentsFile, config.VaultConfigPath},
		})
	}

	return Open(abs, name)
}

// Artifact is one deliverable inside the vault.
type Artifact struct {
	ID      string // 6-character base36
	Slug    string // directory name within the vault
	Type    string // api.DocTypeMD | api.DocTypeHTML
	Dir     string // absolute directory path
	Path    string // absolute entry-file path
	RelPath string // relative to the vault root
	Title   string
}

// Scan scans all artifacts under the vault.
//
// Rule: it walks root's immediate subdirectories (skipping .art, .git, and
// any starting with a dot); a directory containing index.md is type md, one
// containing index.html is type html, and md wins if both are present. The
// id is read from frontmatter aid / <meta name="aid">; when missing it is
// **not** auto-filled (that's the job of art doctor or the watcher) —
// Artifact.ID is left empty and counted as a warning.
func (v *Vault) Scan() ([]Artifact, error) {
	entries, err := os.ReadDir(v.Root)
	if err != nil {
		return nil, err
	}

	arts := []Artifact{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(v.Root, e.Name())
		mdPath := filepath.Join(dir, IndexMD)
		htmlPath := filepath.Join(dir, IndexHTML)

		a := Artifact{Slug: e.Name(), Dir: dir}
		switch {
		case fileExists(mdPath):
			a.Type = api.DocTypeMD
			a.Path = mdPath
		case fileExists(htmlPath):
			a.Type = api.DocTypeHTML
			a.Path = htmlPath
		default:
			continue
		}

		if rel, relErr := filepath.Rel(v.Root, a.Path); relErr == nil {
			a.RelPath = rel
		} else {
			a.RelPath = a.Path
		}

		if content, readErr := os.ReadFile(a.Path); readErr == nil {
			if a.Type == api.DocTypeMD {
				a.ID, a.Title = extractMDMeta(content)
			} else {
				a.ID, a.Title = extractHTMLMeta(content)
			}
		}
		if a.Title == "" {
			a.Title = a.Slug
		}

		arts = append(arts, a)
	}

	sort.Slice(arts, func(i, j int) bool { return arts[i].Slug < arts[j].Slug })
	return arts, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func extractMDMeta(content []byte) (id, title string) {
	if fm, err := mdsrc.ParseFrontmatter(content); err == nil {
		if s, ok := fm[FrontmatterAIDKey].(string); ok {
			id = s
		}
		if s, ok := fm["title"].(string); ok {
			title = s
		}
	}
	if title == "" {
		if doc, err := mdsrc.Parse(content); err == nil {
			for i := range doc.Blocks {
				b := &doc.Blocks[i]
				if b.Kind == "Heading" && b.Level == 1 {
					title = strings.TrimSpace(doc.BlockMap(b).Text)
					break
				}
			}
		}
	}
	return id, title
}

var (
	htmlAIDMetaRe  = regexp.MustCompile(`(?is)<meta\s+[^>]*name=["']aid["'][^>]*content=["']([0-9a-z]+)["'][^>]*>`)
	htmlAIDMetaRe2 = regexp.MustCompile(`(?is)<meta\s+[^>]*content=["']([0-9a-z]+)["'][^>]*name=["']aid["'][^>]*>`)
	htmlTitleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func extractHTMLMeta(content []byte) (id, title string) {
	if m := htmlAIDMetaRe.FindSubmatch(content); m != nil {
		id = string(m[1])
	} else if m := htmlAIDMetaRe2.FindSubmatch(content); m != nil {
		id = string(m[1])
	}
	if m := htmlTitleRe.FindSubmatch(content); m != nil {
		title = strings.TrimSpace(string(m[1]))
	}
	return id, title
}

// Lookup resolves an artifact by slug or id. The id also supports unique prefix matching.
func (v *Vault) Lookup(ref string) (*Artifact, error) {
	arts, err := v.Scan()
	if err != nil {
		return nil, err
	}
	for i := range arts {
		if arts[i].Slug == ref {
			return &arts[i], nil
		}
	}
	for i := range arts {
		if arts[i].ID != "" && arts[i].ID == ref {
			return &arts[i], nil
		}
	}
	var prefixMatch = -1
	for i := range arts {
		if arts[i].ID != "" && strings.HasPrefix(arts[i].ID, ref) {
			if prefixMatch != -1 {
				// Ambiguous prefix: more than one candidate.
				return nil, ErrNotFound
			}
			prefixMatch = i
		}
	}
	if prefixMatch != -1 {
		return &arts[prefixMatch], nil
	}
	return nil, ErrNotFound
}

// New creates a new artifact: assigns an id, creates the directory, and
// writes the skeleton file (md carries aid frontmatter, html carries
// <meta name="aid">). typ must be api.DocTypeMD or api.DocTypeHTML.
func (v *Vault) New(slug, typ, title string) (*Artifact, error) {
	if slug == "" {
		return nil, fmt.Errorf("vault: slug must not be empty")
	}
	if typ != api.DocTypeMD && typ != api.DocTypeHTML {
		return nil, fmt.Errorf("vault: invalid artifact type %q", typ)
	}
	dir := filepath.Join(v.Root, slug)
	if _, err := os.Stat(dir); err == nil {
		return nil, ErrExists
	}
	if title == "" {
		title = slug
	}
	id := idgen.DocID()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var path string
	switch typ {
	case api.DocTypeMD:
		path = filepath.Join(dir, IndexMD)
		content := fmt.Sprintf("---\n%s: %s\ntitle: %s\n---\n\n# %s\n", FrontmatterAIDKey, id, title, title)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
	case api.DocTypeHTML:
		path = filepath.Join(dir, IndexHTML)
		content := fmt.Sprintf(
			"<!doctype html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"%s\" content=\"%s\">\n<title>%s</title>\n</head>\n<body>\n\n</body>\n</html>\n",
			MetaAIDName, id, title,
		)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
	}

	rel, err := filepath.Rel(v.Root, path)
	if err != nil {
		rel = path
	}

	// Commit the skeleton immediately, so git always has a version carrying
	// the aid.
	//
	// The document id lives only in the aid frontmatter / <meta name="aid">;
	// if an agent rewrites this artifact wholesale, it wipes that out, and
	// the watcher's only way to recover it is the last committed version. If
	// the skeleton is never committed and a rewrite happens before the first
	// auto-commit, the id is lost for good.
	//
	// Skipped when git isn't available (vault not under git, or git isn't
	// installed on this machine); creation itself is unaffected.
	if v.Git != nil && v.Git.Available() {
		_, _ = v.Git.Commit(context.Background(), gitx.CommitOptions{
			Message: "art: new " + slug,
			Author:  gitx.AuthorArt,
			Paths:   []string{rel},
		})
	}

	return &Artifact{ID: id, Slug: slug, Type: typ, Dir: dir, Path: path, RelPath: rel, Title: title}, nil
}

// ReadSource reads the content of an artifact's entry file.
func (v *Vault) ReadSource(a *Artifact) ([]byte, error) {
	return os.ReadFile(a.Path)
}

// ResolvePath resolves a vault-relative path to an absolute path, checking
// for path traversal. This is the mandatory choke point for all static file
// reads on the serve side (design doc §9).
func (v *Vault) ResolvePath(rel string) (string, error) {
	if rel == "" {
		rel = "."
	}

	root, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		root = v.Root
	}

	joined := filepath.Join(root, rel)
	if !pathWithin(root, joined) {
		return "", ErrOutsideVault
	}

	// If the target exists, also resolve symlinks along it: a symlink
	// planted inside the vault could otherwise point back out.
	if resolved, err := filepath.EvalSymlinks(joined); err == nil {
		if !pathWithin(root, resolved) {
			return "", ErrOutsideVault
		}
		return resolved, nil
	}
	return joined, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// Docs assembles the list of api.Doc, including open/total comment counts
// and the git rev. baseURL fills Doc.URL, e.g. http://127.0.0.1:7777.
func (v *Vault) Docs(ctx context.Context, baseURL string) ([]api.Doc, error) {
	arts, err := v.Scan()
	if err != nil {
		return nil, err
	}
	docs := make([]api.Doc, 0, len(arts))
	for i := range arts {
		d, err := v.Doc(ctx, &arts[i], baseURL)
		if err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// Doc assembles a single api.Doc.
func (v *Vault) Doc(ctx context.Context, a *Artifact, baseURL string) (api.Doc, error) {
	info, err := os.Stat(a.Path)
	if err != nil {
		return api.Doc{}, err
	}

	var rev string
	if v.Git != nil {
		if r, gerr := v.Git.FileRev(ctx, a.RelPath); gerr == nil {
			rev = r
		}
	}

	var openCount, totalCount int
	if a.ID != "" {
		if fold, ferr := v.Store.Threads(a.ID); ferr == nil {
			for _, th := range fold.Threads {
				totalCount++
				if th.Status == api.StatusOpen {
					openCount++
				}
			}
		}
	}

	var url string
	if baseURL != "" && a.ID != "" {
		url = strings.TrimRight(baseURL, "/") + "/a/" + a.ID
	}

	return api.Doc{
		ID:         a.ID,
		Slug:       a.Slug,
		Title:      a.Title,
		Type:       a.Type,
		Path:       a.Path,
		RelPath:    a.RelPath,
		URL:        url,
		Rev:        rev,
		ModTime:    info.ModTime(),
		Size:       info.Size(),
		OpenCount:  openCount,
		TotalCount: totalCount,
	}, nil
}

// Threads returns a document's folded threads, with Doc/Slug/Path and
// anchor-derived fields already filled in. This is the shared entry point
// for both art comments and GET /api/docs/{id}/comments.
func (v *Vault) Threads(ctx context.Context, a *Artifact) (*api.CommentsResponse, error) {
	fold, err := v.Store.Threads(a.ID)
	if err != nil {
		return nil, err
	}
	threads := make([]api.Thread, len(fold.Threads))
	copy(threads, fold.Threads)
	for i := range threads {
		threads[i].Doc = a.ID
		threads[i].Slug = a.Slug
		threads[i].Path = a.Path
	}

	if a.Type == api.DocTypeMD {
		if src, err := v.ReadSource(a); err == nil {
			if doc, err := mdsrc.Parse(src); err == nil {
				anchor.Enrich(src, doc, threads)
			}
		}
	}

	return &api.CommentsResponse{Doc: a.ID, Threads: threads, Warnings: fold.Warnings}, nil
}

// AllThreads returns threads across all documents, for art comments when
// --doc is not given. An empty status means all threads; otherwise it
// filters by an api.Status* value.
func (v *Vault) AllThreads(ctx context.Context, status string) ([]api.Thread, error) {
	arts, err := v.Scan()
	if err != nil {
		return nil, err
	}
	all := []api.Thread{} // agent-facing --json output; never emit `null` for a zero-length result
	for i := range arts {
		a := &arts[i]
		if a.ID == "" {
			continue
		}
		resp, err := v.Threads(ctx, a)
		if err != nil {
			continue
		}
		for _, th := range resp.Threads {
			if status != "" && th.Status != status {
				continue
			}
			all = append(all, th)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
	return all, nil
}

// FindThread locates a thread and its owning artifact vault-wide by thread
// id (unique prefix matching supported).
func (v *Vault) FindThread(ctx context.Context, threadRef string) (*Artifact, *api.Thread, error) {
	arts, err := v.Scan()
	if err != nil {
		return nil, nil, err
	}

	var exactArt *Artifact
	var exactThread *api.Thread
	var prefixArt *Artifact
	var prefixThread *api.Thread
	ambiguous := false

	for i := range arts {
		a := &arts[i]
		if a.ID == "" {
			continue
		}
		resp, err := v.Threads(ctx, a)
		if err != nil {
			continue
		}
		for j := range resp.Threads {
			th := &resp.Threads[j]
			if th.Thread == threadRef {
				exactArt, exactThread = a, th
			} else if strings.HasPrefix(th.Thread, threadRef) {
				if prefixThread != nil {
					ambiguous = true
				}
				prefixArt, prefixThread = a, th
			}
		}
	}

	if exactThread != nil {
		return exactArt, exactThread, nil
	}
	if prefixThread != nil && !ambiguous {
		return prefixArt, prefixThread, nil
	}
	return nil, nil, ErrNotFound
}

// Author returns the current commenter identity: vault config Author > $ART_AUTHOR > $USER.
func (v *Vault) Author() string {
	if v.Cfg != nil && v.Cfg.Author != "" {
		return v.Cfg.Author
	}
	if a := os.Getenv("ART_AUTHOR"); a != "" {
		return a
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "unknown"
}

// AgentsTemplate is the AGENTS.md content that art init writes (design doc §10, verbatim).
func AgentsTemplate() []byte {
	return []byte(`# This project's deliverables are published via art

- When producing a proposal/demo: run ` + "`art new <slug> --type md|html --json`" + ` to get a
  path, then write the content there yourself with your own file tools; your reply
  must include the returned url.
- At the start of a session run ` + "`art comments --open --json`" + `, and handle each thread:
  edit the doc → ` + "`art reply <thread> \"<explanation>\"`" + ` → ` + "`art addressed <thread> --commit <sha>`" + `.
  Never resolve threads yourself — resolving is for the human.
- For threads in orphan status: confirm whether the original concern was addressed, then
  reply with an explanation and ask the human to confirm.
- data-aid / aid frontmatter are system-managed identifiers — preserve them verbatim when editing.
`)
}

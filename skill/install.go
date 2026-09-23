package skill

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/seanly/dmr-devkit/tool"
)

const (
	defaultMaxInstallDownload int64 = 2 * 1024 * 1024
	maxInstallRedirects             = 5
	skillOriginFile                 = ".skill-origin.json"
	skillOriginVersion              = 1
)

var (
	skillInstallMu sync.Mutex
	githubNameRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// skillInstallOrigin records where a URL-installed skill came from.
type skillInstallOrigin struct {
	Version     int    `json:"version"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	InstalledAt int64  `json:"installedAt"`
}

type installKind int

const (
	installKindUnknown installKind = iota
	installKindFile
	installKindZip
	installKindTarGz
	installKindGitHubTree
)

type installSource struct {
	Kind   installKind
	Fetch  *url.URL
	Subdir string
}

func (m *Manager) skillInstallTool() *tool.Tool {
	return &tool.Tool{
		Spec: tool.ToolSpec{
			Name:        "skillInstall",
			Description: "Install a skill from one https URL. Accepts a raw SKILL.md URL, a GitHub blob or tree URL, a zip archive, or a .tar.gz/.tgz whose single top-level directory contains SKILL.md. The skill is saved under the auto-create directory with group forced to extended.",
			Group:       m.toolGroup,
			SearchHint:  "skill, install, url, download, github, zip, tar.gz, SKILL.md, 安装, 下载",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "https URL of a SKILL.md file, a GitHub blob/tree path, a .zip archive, or a .tar.gz/.tgz skill directory",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Optional directory name. Defaults to the skill frontmatter name.",
					},
					"force": map[string]any{
						"type":        "boolean",
						"description": "If true, replace an existing skill directory under the auto-create path.",
					},
				},
				"required": []string{"url"},
			},
		},
		Handler: m.handleSkillInstall,
	}
}

func (m *Manager) handleSkillInstall(_ *tool.ToolContext, args map[string]any) (any, error) {
	if !m.config.AllowCreate {
		return skillInstallFail("skill creation is disabled"), nil
	}
	if strings.TrimSpace(m.config.AutoCreatePath) == "" {
		return skillInstallFail("auto_create_path is not configured"), nil
	}

	rawURL, _ := args["url"].(string)
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return skillInstallFail("url is required"), nil
	}
	nameOverride, _ := args["name"].(string)
	force, _ := args["force"].(bool)

	src, err := classifyInstallURL(rawURL)
	if err != nil {
		return skillInstallFail(err.Error()), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	body, contentType, err := m.downloadInstall(ctx, src.Fetch)
	if err != nil {
		return skillInstallFail(err.Error()), nil
	}

	var cleanup string
	var skills []preparedSkill
	switch {
	case src.asTarGz(contentType, body):
		cleanup, skills, err = m.stageTarGzSkill(body)
	case src.asZip(contentType, body):
		cleanup, skills, err = m.stageZipSkill(body, src)
	default:
		if isHTMLContent(contentType) {
			return skillInstallFail("URL is an HTML page, not a SKILL.md, zip, or tar.gz"), nil
		}
		cleanup, skills, err = m.stageFileSkill(body)
	}
	if err != nil {
		return skillInstallFail(err.Error()), nil
	}
	if cleanup != "" {
		defer os.RemoveAll(cleanup)
	}
	return m.commitInstalledSkills(skills, strings.TrimSpace(nameOverride), force, rawURL)
}

type preparedSkill struct {
	root    string
	content string
}

func (m *Manager) commitInstalledSkills(skills []preparedSkill, nameOverride string, force bool, rawURL string) (any, error) {
	if len(skills) == 0 {
		return skillInstallFail("archive must contain SKILL.md"), nil
	}
	if len(skills) > 1 && nameOverride != "" {
		return skillInstallFail("name is only allowed when the archive contains one skill"), nil
	}

	type installItem struct {
		root    string
		content string
		dirName string
		dest    string
	}
	items := make([]installItem, 0, len(skills))
	seen := make(map[string]struct{}, len(skills))
	override := ""
	if len(skills) == 1 {
		override = nameOverride
	}
	for _, sk := range skills {
		dirName, err := installDirName(sk.content, override)
		if err != nil {
			return skillInstallFail(err.Error()), nil
		}
		if _, ok := seen[dirName]; ok {
			return skillInstallFail(fmt.Sprintf("archive contains more than one skill named %q", dirName)), nil
		}
		seen[dirName] = struct{}{}
		dest, err := installDest(m.config.AutoCreatePath, dirName)
		if err != nil {
			return skillInstallFail(err.Error()), nil
		}
		items = append(items, installItem{root: sk.root, content: sk.content, dirName: dirName, dest: dest})
	}

	skillInstallMu.Lock()
	defer skillInstallMu.Unlock()

	if !force {
		for _, it := range items {
			if _, statErr := os.Stat(it.dest); statErr == nil {
				return skillInstallFail(fmt.Sprintf("skill %q already installed at %s (use force=true to reinstall)", it.dirName, it.dest)), nil
			}
		}
	}

	type stagedItem struct {
		installItem
		dir string
	}
	staged := make([]stagedItem, 0, len(items))
	removeStaged := true
	defer func() {
		if !removeStaged {
			return
		}
		for _, s := range staged {
			_ = os.RemoveAll(s.dir)
		}
	}()
	for _, it := range items {
		if err := os.MkdirAll(filepath.Dir(it.dest), 0o755); err != nil {
			return skillInstallFail(err.Error()), nil
		}
		dir, err := os.MkdirTemp(filepath.Dir(it.dest), ".install-*")
		if err != nil {
			return skillInstallFail(err.Error()), nil
		}
		staged = append(staged, stagedItem{installItem: it, dir: dir})
		if err := copySkillFiles(it.root, dir, []byte(it.content)); err != nil {
			return skillInstallFail(err.Error()), nil
		}
		if err := writeSkillOrigin(dir, rawURL); err != nil {
			return skillInstallFail(err.Error()), nil
		}
	}

	committed := make([]string, 0, len(staged))
	rollback := func() {
		for _, dest := range committed {
			_ = os.RemoveAll(dest)
		}
	}
	for _, s := range staged {
		if force {
			if _, statErr := os.Stat(s.dest); statErr == nil {
				if err := os.RemoveAll(s.dest); err != nil {
					rollback()
					return skillInstallFail(err.Error()), nil
				}
			}
		}
		if err := os.Rename(s.dir, s.dest); err != nil {
			rollback()
			return skillInstallFail(err.Error()), nil
		}
		committed = append(committed, s.dest)
	}
	removeStaged = false

	m.cleanupAutoSkills()
	m.refreshSkills()

	installed := make([]any, 0, len(staged))
	for _, s := range staged {
		installed = append(installed, map[string]any{
			"name":     s.dirName,
			"location": filepath.Join(s.dest, "SKILL.md"),
		})
	}
	result := map[string]any{
		"success":   true,
		"url":       rawURL,
		"installed": installed,
	}
	if len(staged) == 1 {
		result["name"] = staged[0].dirName
		result["location"] = filepath.Join(staged[0].dest, "SKILL.md")
	}
	return result, nil
}

func skillInstallFail(msg string) map[string]any {
	return map[string]any{"success": false, "error": msg}
}

func (k installSource) asZip(contentType string, body []byte) bool {
	switch k.Kind {
	case installKindZip, installKindGitHubTree:
		return true
	case installKindFile, installKindTarGz:
		return false
	default:
		return zipContentType(contentType) || looksLikeZip(body)
	}
}

func (k installSource) asTarGz(contentType string, body []byte) bool {
	switch k.Kind {
	case installKindTarGz:
		return true
	case installKindFile, installKindZip, installKindGitHubTree:
		return false
	default:
		return tarGzContentType(contentType) || looksLikeGzip(body)
	}
}

func classifyInstallURL(raw string) (installSource, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return installSource{}, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "https" {
		return installSource{}, fmt.Errorf("only https URLs are allowed")
	}
	if u.Hostname() == "" {
		return installSource{}, fmt.Errorf("url host is required")
	}
	u.Fragment = ""

	if strings.EqualFold(u.Hostname(), "github.com") {
		src, ok, err := classifyGitHubURL(u)
		if err != nil {
			return installSource{}, err
		}
		if ok {
			return src, nil
		}
	}

	base := strings.ToLower(pathBase(u.Path))
	switch {
	case base == "skill.md":
		return installSource{Kind: installKindFile, Fetch: u}, nil
	case strings.HasSuffix(base, ".zip"):
		return installSource{Kind: installKindZip, Fetch: u}, nil
	case strings.HasSuffix(base, ".tar.gz"), strings.HasSuffix(base, ".tgz"):
		return installSource{Kind: installKindTarGz, Fetch: u}, nil
	default:
		return installSource{Kind: installKindUnknown, Fetch: u}, nil
	}
}

func classifyGitHubURL(u *url.URL) (installSource, bool, error) {
	parts := splitURLPath(u.Path)
	// owner / repo / blob|tree / ref / rest...
	if len(parts) < 4 {
		return installSource{}, false, nil
	}
	owner, repo, kind, ref := parts[0], parts[1], parts[2], parts[3]
	rest := strings.Join(parts[4:], "/")
	if !githubNameRe.MatchString(owner) || !githubNameRe.MatchString(repo) {
		return installSource{}, false, fmt.Errorf("invalid GitHub owner or repo")
	}
	if !validGitRef(ref) {
		return installSource{}, false, fmt.Errorf("invalid GitHub ref %q", ref)
	}

	switch kind {
	case "blob":
		if rest == "" || !strings.EqualFold(pathBase(rest), "SKILL.md") {
			return installSource{}, false, fmt.Errorf("GitHub blob URL must point at SKILL.md")
		}
		fetch := &url.URL{Scheme: "https", Host: "raw.githubusercontent.com"}
		fetch = fetch.JoinPath(append([]string{owner, repo, ref}, parts[4:]...)...)
		return installSource{Kind: installKindFile, Fetch: fetch}, true, nil
	case "tree":
		if rest != "" {
			if err := validateSkillBundlePath(rest); err != nil {
				return installSource{}, false, err
			}
		}
		fetch := &url.URL{Scheme: "https", Host: "codeload.github.com"}
		fetch = fetch.JoinPath(owner, repo, "zip", ref)
		return installSource{Kind: installKindGitHubTree, Fetch: fetch, Subdir: rest}, true, nil
	default:
		return installSource{}, false, nil
	}
}

func validGitRef(ref string) bool {
	if ref == "" || ref == "." || ref == ".." || strings.Contains(ref, "..") {
		return false
	}
	if strings.ContainsAny(ref, `/\?#`) {
		return false
	}
	return true
}

func splitURLPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func pathBase(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func (m *Manager) stageFileSkill(body []byte) (cleanup string, skills []preparedSkill, err error) {
	content, err := m.prepareSkillMarkdown(string(body))
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "dmr-skill-install-*")
	if err != nil {
		return "", nil, err
	}
	if err = writeFileAtomic(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, []preparedSkill{{root: dir, content: content}}, nil
}

func (m *Manager) stageZipSkill(body []byte, src installSource) (cleanup string, skills []preparedSkill, err error) {
	dir, err := os.MkdirTemp("", "dmr-skill-install-*")
	if err != nil {
		return "", nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	extracted := filepath.Join(dir, "zip")
	if err = os.MkdirAll(extracted, 0o755); err != nil {
		return "", nil, err
	}
	if err = extractZipSafe(body, extracted, m.installLimit()); err != nil {
		return "", nil, err
	}
	var roots []string
	if src.Kind == installKindGitHubTree {
		var root string
		root, err = locateSkillRoot(extracted, src)
		if err != nil {
			return "", nil, err
		}
		roots = []string{root}
	} else {
		roots, err = findSkillRoots(extracted)
		if err != nil {
			return "", nil, err
		}
	}
	skills, err = m.prepareSkillRoots(roots)
	if err != nil {
		return "", nil, err
	}
	return dir, skills, nil
}

func (m *Manager) stageTarGzSkill(body []byte) (cleanup string, skills []preparedSkill, err error) {
	dir, err := os.MkdirTemp("", "dmr-skill-install-*")
	if err != nil {
		return "", nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	extracted := filepath.Join(dir, "tar")
	if err = os.MkdirAll(extracted, 0o755); err != nil {
		return "", nil, err
	}
	if err = extractTarGzSafe(body, extracted, m.installLimit()); err != nil {
		return "", nil, err
	}
	roots, err := findSkillRoots(extracted)
	if err != nil {
		return "", nil, err
	}
	skills, err = m.prepareSkillRoots(roots)
	if err != nil {
		return "", nil, err
	}
	return dir, skills, nil
}

func (m *Manager) prepareSkillRoots(roots []string) ([]preparedSkill, error) {
	skills := make([]preparedSkill, 0, len(roots))
	for _, root := range roots {
		content, err := m.normalizeSkillFile(root)
		if err != nil {
			return nil, err
		}
		skills = append(skills, preparedSkill{root: root, content: content})
	}
	return skills, nil
}

func (m *Manager) normalizeSkillFile(root string) (string, error) {
	mdPath, err := findSkillMarkdown(root)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(mdPath)
	if err != nil {
		return "", err
	}
	content, err := m.prepareSkillMarkdown(string(raw))
	if err != nil {
		return "", err
	}
	if err = writeFileAtomic(mdPath, []byte(content), 0o600); err != nil {
		return "", err
	}
	if filepath.Base(mdPath) != "SKILL.md" {
		if err = os.Rename(mdPath, filepath.Join(filepath.Dir(mdPath), "SKILL.md")); err != nil {
			return "", err
		}
	}
	return content, nil
}

func (m *Manager) prepareSkillMarkdown(content string) (string, error) {
	if err := validateSkillContent(content, m.config.MaxSkillSize); err != nil {
		return "", err
	}
	if m.config.SecurityScan {
		if err := scanSkillContent(content); err != nil {
			return "", err
		}
	}
	return normalizeSkillGroup(content, "extended"), nil
}

func installDirName(content, nameOverride string) (string, error) {
	sk, err := ParseSkillMarkdown([]byte(content), "SKILL.md")
	if err != nil {
		return "", err
	}
	dirName := safeName(sk.Name)
	if override := strings.TrimSpace(nameOverride); override != "" {
		dirName = safeName(override)
	}
	if strings.Trim(dirName, "-") == "" {
		return "", fmt.Errorf("invalid skill name")
	}
	return dirName, nil
}

func installDest(autoRoot, dirName string) (string, error) {
	root, err := filepath.Abs(autoRoot)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(root, dirName)
	dest, err = filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if dest == root || !isPathUnderRoot(dest, root) {
		return "", fmt.Errorf("refusing to install outside auto_create_path")
	}
	return dest, nil
}

func writeSkillOrigin(dir, rawURL string) error {
	origin := skillInstallOrigin{
		Version:     skillOriginVersion,
		Source:      "url",
		URL:         rawURL,
		InstalledAt: time.Now().UnixMilli(),
	}
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(dir, skillOriginFile), data, 0o600)
}

func copySkillFiles(src, dst string, skillMD []byte) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	wroteSkill := false
	err = filepath.WalkDir(srcAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if err := validateSkillBundlePath(relSlash); err != nil {
			return err
		}
		target := filepath.Join(dstAbs, rel)
		if !isPathUnderRoot(target, dstAbs) {
			return fmt.Errorf("file path escapes skill dir: %s", relSlash)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", relSlash)
		}
		if strings.EqualFold(d.Name(), "SKILL.md") && filepath.Dir(path) == srcAbs {
			if err := writeFileAtomic(filepath.Join(dstAbs, "SKILL.md"), skillMD, 0o600); err != nil {
				return err
			}
			wroteSkill = true
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFileMode(path, target, 0o600)
	})
	if err != nil {
		return err
	}
	if !wroteSkill {
		return fmt.Errorf("SKILL.md not found")
	}
	return nil
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, in)
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpName)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmpName)
		return closeErr
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, dst)
}

func locateSkillRoot(extracted string, src installSource) (string, error) {
	extracted, err := filepath.Abs(extracted)
	if err != nil {
		return "", err
	}
	if src.Kind == installKindGitHubTree {
		base := extracted
		if child, err := singleChildDir(extracted); err == nil {
			base = filepath.Join(extracted, child)
		}
		root := base
		if src.Subdir != "" {
			if err := validateSkillBundlePath(src.Subdir); err != nil {
				return "", err
			}
			root = filepath.Join(base, filepath.FromSlash(src.Subdir))
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return "", err
		}
		if root != extracted && !isPathUnderRoot(root, extracted) {
			return "", fmt.Errorf("skill path escapes archive")
		}
		if _, err := findSkillMarkdown(root); err != nil {
			return "", fmt.Errorf("SKILL.md not found in GitHub directory")
		}
		return root, nil
	}
	if _, err := findSkillMarkdown(extracted); err == nil {
		return extracted, nil
	}
	child, err := singleChildDir(extracted)
	if err != nil {
		return "", fmt.Errorf("installed skill must contain SKILL.md at root")
	}
	nested := filepath.Join(extracted, child)
	if _, err := findSkillMarkdown(nested); err != nil {
		return "", fmt.Errorf("SKILL.md not found under %q", child)
	}
	return nested, nil
}

func singleChildDir(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var dirs []string
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
			continue
		}
		files++
	}
	if files == 0 && len(dirs) == 1 {
		return dirs[0], nil
	}
	return "", fmt.Errorf("archive does not have a single top-level directory")
}

// findSkillRoots returns skill directories inside an extracted zip or tar.gz.
// A single wrapper directory is stripped when it has no SKILL.md of its own.
// Immediate child directories that contain SKILL.md are each a skill. When none
// do, the current directory is one skill if it contains SKILL.md.
func findSkillRoots(extracted string) ([]string, error) {
	base, err := filepath.Abs(extracted)
	if err != nil {
		return nil, err
	}
	if _, err := findSkillMarkdown(base); err != nil {
		if child, serr := singleChildDir(base); serr == nil {
			base = filepath.Join(base, child)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(base, e.Name())
		if _, err := findSkillMarkdown(child); err != nil {
			continue
		}
		roots = append(roots, child)
	}
	if len(roots) > 0 {
		sort.Slice(roots, func(i, j int) bool {
			return filepath.Base(roots[i]) < filepath.Base(roots[j])
		})
		return roots, nil
	}
	if _, err := findSkillMarkdown(base); err == nil {
		return []string{base}, nil
	}
	return nil, fmt.Errorf("archive must contain SKILL.md")
}

func findSkillMarkdown(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(e.Name(), "SKILL.md") {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("SKILL.md not found")
}

func extractZipSafe(data []byte, dest string, limit int64) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid zip: %w", err)
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = defaultMaxInstallDownload
	}
	var written int64
	for _, f := range zr.File {
		if strings.Contains(f.Name, `\`) {
			return fmt.Errorf("unsafe zip path %q", f.Name)
		}
		name := filepath.ToSlash(filepath.Clean(f.Name))
		if name == "." || name == "" {
			continue
		}
		if err := validateSkillBundlePath(name); err != nil {
			return fmt.Errorf("unsafe zip path %q: %w", f.Name, err)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip symlink not allowed: %s", f.Name)
		}
		target := filepath.Join(destAbs, filepath.FromSlash(name))
		if !isPathUnderRoot(target, destAbs) {
			return fmt.Errorf("zip entry escapes target: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			rc.Close()
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(rc, limit-written+1))
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		written += n
		if written > limit {
			return fmt.Errorf("extracted skill exceeds %d bytes", limit)
		}
	}
	return nil
}

func extractTarGzSafe(data []byte, dest string, limit int64) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = defaultMaxInstallDownload
	}
	var written int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid tar: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("tar link not allowed: %s", hdr.Name)
		default:
			return fmt.Errorf("unsupported tar entry %q", hdr.Name)
		}
		name, err := cleanArchiveName(hdr.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		target := filepath.Join(destAbs, filepath.FromSlash(name))
		if !isPathUnderRoot(target, destAbs) {
			return fmt.Errorf("tar entry escapes target: %q", hdr.Name)
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(out, io.LimitReader(tr, limit-written+1))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		written += n
		if written > limit {
			return fmt.Errorf("extracted skill exceeds %d bytes", limit)
		}
	}
}

func cleanArchiveName(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	for strings.HasPrefix(name, "/") {
		name = strings.TrimPrefix(name, "/")
	}
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	name = filepath.ToSlash(filepath.Clean(name))
	if name == "." || name == "" {
		return "", nil
	}
	if err := validateSkillBundlePath(name); err != nil {
		return "", fmt.Errorf("unsafe archive path %q: %w", name, err)
	}
	return name, nil
}

func (m *Manager) downloadInstall(ctx context.Context, u *url.URL) ([]byte, string, error) {
	if err := m.checkInstallURL(u); err != nil {
		return nil, "", err
	}
	fetch := m.mapInstallHost(u)
	if fetch.String() != u.String() {
		if err := m.checkInstallURL(fetch); err != nil {
			return nil, "", err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetch.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "dmr-skill-install")

	resp, err := m.installHTTPClient().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	limit := m.installLimit()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(buf)) > limit {
		return nil, "", fmt.Errorf("download exceeds %d bytes", limit)
	}
	if len(buf) == 0 {
		return nil, "", fmt.Errorf("download was empty")
	}
	return buf, resp.Header.Get("Content-Type"), nil
}

func (m *Manager) installLimit() int64 {
	if m.installMaxDownload > 0 {
		return m.installMaxDownload
	}
	return defaultMaxInstallDownload
}

func (m *Manager) installHTTPClient() *http.Client {
	if m.installClient != nil {
		c := *m.installClient
		c.CheckRedirect = m.installCheckRedirect
		if c.Timeout == 0 {
			c.Timeout = 60 * time.Second
		}
		return &c
	}
	return &http.Client{
		Timeout:       60 * time.Second,
		Transport:     m.installTransport(),
		CheckRedirect: m.installCheckRedirect,
	}
}

func (m *Manager) installCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxInstallRedirects {
		return fmt.Errorf("stopped after %d redirects", maxInstallRedirects)
	}
	return m.checkInstallURL(req.URL)
}

func (m *Manager) installTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip, err := m.resolveInstallIP(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          2,
	}
}

func (m *Manager) mapInstallHost(u *url.URL) *url.URL {
	if u == nil || len(m.installHostMap) == 0 {
		return u
	}
	repl, ok := m.installHostMap[u.Hostname()]
	if !ok || repl == "" {
		return u
	}
	c := *u
	c.Host = repl
	return &c
}

func (m *Manager) checkInstallURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" {
		return fmt.Errorf("only https URLs are allowed")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	if isMetadataHost(host) {
		return fmt.Errorf("refusing to fetch %s", host)
	}
	if m.installAllowPrivate {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("refusing to fetch %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedInstallIP(ip) {
			return fmt.Errorf("refusing to fetch %s", host)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.resolveInstallIP(ctx, host); err != nil {
		return err
	}
	return nil
}

func (m *Manager) resolveInstallIP(ctx context.Context, host string) (net.IP, error) {
	if isMetadataHost(host) {
		return nil, fmt.Errorf("refusing to fetch %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedInstallIP(ip) && !m.installAllowPrivate {
			return nil, fmt.Errorf("refusing to fetch %s", host)
		}
		return ip, nil
	}
	if strings.EqualFold(host, "localhost") && !m.installAllowPrivate {
		return nil, fmt.Errorf("refusing to fetch %s", host)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	if !m.installAllowPrivate {
		for _, ip := range ips {
			if isBlockedInstallIP(ip.IP) {
				return nil, fmt.Errorf("refusing to fetch %s", host)
			}
		}
	}
	return ips[0].IP, nil
}

func isMetadataHost(host string) bool {
	switch strings.ToLower(host) {
	case "metadata.google.internal", "metadata.goog", "169.254.169.254":
		return true
	default:
		return false
	}
}

func isBlockedInstallIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

func zipContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "application/zip", "application/x-zip-compressed", "application/x-zip":
		return true
	default:
		return false
	}
}

func looksLikeZip(b []byte) bool {
	return len(b) >= 4 && b[0] == 'P' && b[1] == 'K'
}

func tarGzContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "application/gzip", "application/x-gzip", "application/x-gtar", "application/x-compressed-tar":
		return true
	default:
		return false
	}
}

func looksLikeGzip(b []byte) bool {
	return len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b
}

func isHTMLContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html")
}

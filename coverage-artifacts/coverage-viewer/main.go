package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var profileLineRE = regexp.MustCompile(`^(.*):([0-9]+)\.[0-9]+,([0-9]+)\.[0-9]+\s+[0-9]+\s+([0-9]+)$`)

type lineCoverage struct {
	Executable map[int]struct{}
	Covered    map[int]struct{}
}

type testCoverage struct {
	Files map[string]*lineCoverage
}

type app struct {
	repoRoot    string
	resultsDir  string
	modulePath  string
	cacheMu     sync.RWMutex
	coverageMap map[string]*testCoverage
}

type fileSummary struct {
	Path       string  `json:"path"`
	Covered    int     `json:"covered"`
	Executable int     `json:"executable"`
	Percent    float64 `json:"percent"`
}

type lineData struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

type sourceResponse struct {
	File       string     `json:"file"`
	Covered    int        `json:"covered"`
	Executable int        `json:"executable"`
	Percent    float64    `json:"percent"`
	Lines      []lineData `json:"lines"`
}

type functionsByFileJSON struct {
	Test  string                 `json:"test"`
	Files []functionsByFileEntry `json:"files"`
}

type functionsByFileEntry struct {
	Path             string   `json:"path"`
	CoveredFunctions []string `json:"covered_functions"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "HTTP listen address")
	repoRoot := flag.String("repo-root", ".", "path to snapd repository root")
	resultsDir := flag.String("results-dir", "coverage-artifacts/coverage-results", "path to raw coverage result directories")
	listCoveredFiles := flag.Bool("list-covered-files", false, "print files with at least one covered line for a given test and exit")
	functionsJSON := flag.Bool("functions-json", false, "print JSON with covered functions per file for a given test and exit")
	testName := flag.String("test", "", "test directory name under coverage-results (required with -list-covered-files and -functions-json)")
	flag.Parse()

	absRepoRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		log.Fatalf("cannot resolve repo root: %v", err)
	}

	modulePath, err := readModulePath(absRepoRoot)
	if err != nil {
		log.Fatalf("cannot read go module path: %v", err)
	}

	absResultsDir := *resultsDir
	if !filepath.IsAbs(absResultsDir) {
		absResultsDir = filepath.Join(absRepoRoot, absResultsDir)
	}
	absResultsDir, err = filepath.Abs(absResultsDir)
	if err != nil {
		log.Fatalf("cannot resolve results dir: %v", err)
	}
	if err := ensureDir(absResultsDir); err != nil {
		log.Fatalf("coverage results dir is not accessible: %v", err)
	}

	a := &app{
		repoRoot:    absRepoRoot,
		resultsDir:  absResultsDir,
		modulePath:  modulePath,
		coverageMap: make(map[string]*testCoverage),
	}

	if *listCoveredFiles {
		if strings.TrimSpace(*testName) == "" {
			log.Fatalf("cannot list covered files: provide -test")
		}
		if err := a.printCoveredFiles(*testName); err != nil {
			log.Fatalf("cannot list covered files: %v", err)
		}
		return
	}

	if *functionsJSON {
		if strings.TrimSpace(*testName) == "" {
			log.Fatalf("cannot print functions JSON: provide -test")
		}
		if err := a.printFunctionsJSON(*testName); err != nil {
			log.Fatalf("cannot print functions JSON: %v", err)
		}
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/tests", a.handleTests)
	mux.HandleFunc("/api/files", a.handleFiles)
	mux.HandleFunc("/api/source", a.handleSource)

	log.Printf("coverage viewer listening on http://%s", *addr)
	log.Printf("repo root: %s", absRepoRoot)
	log.Printf("coverage results: %s", absResultsDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("cannot start server: %v", err)
	}
}

func (a *app) printCoveredFiles(testName string) error {
	coverage, err := a.loadCoverage(testName)
	if err != nil {
		return err
	}

	coveredFiles := make([]string, 0, len(coverage.Files))
	for path, fileCov := range coverage.Files {
		if len(fileCov.Covered) > 0 {
			coveredFiles = append(coveredFiles, path)
		}
	}
	sort.Strings(coveredFiles)

	for _, path := range coveredFiles {
		fmt.Println(path)
	}
	return nil
}

func (a *app) printFunctionsJSON(testName string) error {
	coverage, err := a.loadCoverage(testName)
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(coverage.Files))
	for path, fileCov := range coverage.Files {
		if len(fileCov.Covered) > 0 {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	payload := functionsByFileJSON{
		Test:  testName,
		Files: make([]functionsByFileEntry, 0, len(paths)),
	}

	for _, path := range paths {
		fileCov := coverage.Files[path]
		covered, err := a.extractCoveredFunctions(path, fileCov)
		if err != nil {
			return err
		}
		payload.Files = append(payload.Files, functionsByFileEntry{
			Path:             path,
			CoveredFunctions: covered,
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func (a *app) extractCoveredFunctions(filePath string, fileCov *lineCoverage) ([]string, error) {
	sourcePath, err := a.sourcePath(filePath)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, sourcePath, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %v", filePath, err)
	}

	coveredSet := make(map[string]struct{})
	for _, decl := range astFile.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Body.Lbrace).Line
		end := fset.Position(fn.Body.Rbrace).Line
		if !hasCoveredLineInRange(fileCov.Covered, start, end) {
			continue
		}
		coveredSet[funcDeclName(fset, fn)] = struct{}{}
	}

	covered := mapKeysSorted(coveredSet)
	return covered, nil
}

func hasCoveredLineInRange(covered map[int]struct{}, start, end int) bool {
	for ln := range covered {
		if ln >= start && ln <= end {
			return true
		}
	}
	return false
}

func mapKeysSorted(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, n); err != nil {
		return "<unknown>"
	}
	return strings.Join(strings.Fields(strings.TrimSpace(buf.String())), " ")
}

func callExprName(fset *token.FileSet, expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		left := callExprName(fset, e.X)
		if left == "" || left == "<unknown>" {
			return e.Sel.Name
		}
		return left + "." + e.Sel.Name
	case *ast.ParenExpr:
		return callExprName(fset, e.X)
	case *ast.StarExpr:
		return callExprName(fset, e.X)
	case *ast.UnaryExpr:
		return callExprName(fset, e.X)
	case *ast.CallExpr:
		return callExprName(fset, e.Fun) + "()"
	case *ast.IndexExpr:
		return callExprName(fset, e.X)
	case *ast.IndexListExpr:
		return callExprName(fset, e.X)
	case *ast.CompositeLit:
		if e.Type != nil {
			return callExprName(fset, e.Type)
		}
		return "<composite>"
	case *ast.FuncLit:
		return "<func-literal>"
	default:
		return formatNode(fset, expr)
	}
}

func funcDeclName(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := formatNode(fset, fn.Recv.List[0].Type)
	return fmt.Sprintf("(%s).%s", recv, fn.Name.Name)
}

func readModulePath(repoRoot string) (string, error) {
	goModPath := filepath.Join(repoRoot, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			modulePath := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if modulePath == "" {
				break
			}
			return modulePath, nil
		}
	}
	return "", errors.New("module path not found in go.mod")
}

func ensureDir(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (a *app) handleTests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := os.ReadDir(a.resultsDir)
	if err != nil {
		http.Error(w, "cannot list tests", http.StatusInternalServerError)
		log.Printf("cannot list coverage results: %v", err)
		return
	}
	tests := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			tests = append(tests, e.Name())
		}
	}
	sort.Strings(tests)
	writeJSON(w, map[string]any{"tests": tests})
}

func (a *app) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	testName := r.URL.Query().Get("test")
	if testName == "" {
		http.Error(w, "missing test query parameter", http.StatusBadRequest)
		return
	}
	coverage, err := a.loadCoverage(testName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	summaries := make([]fileSummary, 0, len(coverage.Files))
	for path, fileCov := range coverage.Files {
		covered, executable, pct := coverageTotals(fileCov)
		summaries = append(summaries, fileSummary{
			Path:       path,
			Covered:    covered,
			Executable: executable,
			Percent:    pct,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Path < summaries[j].Path
	})
	writeJSON(w, map[string]any{"files": summaries})
}

func (a *app) handleSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	testName := r.URL.Query().Get("test")
	filePath := r.URL.Query().Get("file")
	if testName == "" || filePath == "" {
		http.Error(w, "missing test or file query parameter", http.StatusBadRequest)
		return
	}

	coverage, err := a.loadCoverage(testName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fileCov, ok := coverage.Files[filePath]
	if !ok {
		http.Error(w, "file not present in selected test coverage", http.StatusNotFound)
		return
	}

	sourcePath, err := a.sourcePath(filePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		http.Error(w, "cannot read source file", http.StatusNotFound)
		return
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	lineRows := make([]lineData, 0, len(lines))
	for i, line := range lines {
		lineNo := i + 1
		status := "neutral"
		if _, ok := fileCov.Executable[lineNo]; ok {
			status = "uncovered"
			if _, ok := fileCov.Covered[lineNo]; ok {
				status = "covered"
			}
		}
		lineRows = append(lineRows, lineData{Number: lineNo, Text: line, Status: status})
	}
	covered, executable, pct := coverageTotals(fileCov)
	writeJSON(w, sourceResponse{
		File:       filePath,
		Covered:    covered,
		Executable: executable,
		Percent:    pct,
		Lines:      lineRows,
	})
}

func (a *app) loadCoverage(testName string) (*testCoverage, error) {
	if strings.ContainsRune(testName, filepath.Separator) || testName == "." || testName == ".." {
		return nil, errors.New("invalid test name")
	}

	a.cacheMu.RLock()
	cached, ok := a.coverageMap[testName]
	a.cacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	testDir := filepath.Join(a.resultsDir, testName)
	if err := ensureDir(testDir); err != nil {
		return nil, fmt.Errorf("unknown test: %s", testName)
	}

	profilePath, err := a.runCovdataTextfmt(testDir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(profilePath)

	parsed, err := a.parseProfile(profilePath)
	if err != nil {
		return nil, err
	}

	a.cacheMu.Lock()
	a.coverageMap[testName] = parsed
	a.cacheMu.Unlock()
	return parsed, nil
}

func (a *app) runCovdataTextfmt(testDir string) (string, error) {
	tmpFile, err := os.CreateTemp("", "snapd-cov-profile-*.out")
	if err != nil {
		return "", fmt.Errorf("cannot create temporary profile file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("cannot close temporary profile file: %v", err)
	}

	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i", testDir, "-o", tmpFile.Name())
	cmd.Dir = a.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("cannot convert raw coverage for %q: %v (%s)", filepath.Base(testDir), err, strings.TrimSpace(string(output)))
	}
	return tmpFile.Name(), nil
}

func (a *app) parseProfile(profilePath string) (*testCoverage, error) {
	file, err := os.Open(profilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open profile: %v", err)
	}
	defer file.Close()

	result := &testCoverage{Files: make(map[string]*lineCoverage)}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		matches := profileLineRE.FindStringSubmatch(line)
		if len(matches) != 5 {
			continue
		}

		path := a.normalizeProfilePath(matches[1])
		if path == "" {
			continue
		}

		startLine, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}
		endLine, err := strconv.Atoi(matches[3])
		if err != nil {
			continue
		}
		count, err := strconv.ParseInt(matches[4], 10, 64)
		if err != nil {
			continue
		}
		if endLine < startLine {
			startLine, endLine = endLine, startLine
		}
		fileCov := result.Files[path]
		if fileCov == nil {
			fileCov = &lineCoverage{Executable: make(map[int]struct{}), Covered: make(map[int]struct{})}
			result.Files[path] = fileCov
		}
		for ln := startLine; ln <= endLine; ln++ {
			fileCov.Executable[ln] = struct{}{}
			if count > 0 {
				fileCov.Covered[ln] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot scan profile: %v", err)
	}
	return result, nil
}

func (a *app) normalizeProfilePath(path string) string {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	modulePrefix := strings.TrimSuffix(a.modulePath, "/") + "/"
	clean = strings.TrimPrefix(clean, modulePrefix)
	clean = strings.TrimPrefix(clean, "./")
	clean = filepath.ToSlash(filepath.Clean(clean))
	if clean == "." || strings.HasPrefix(clean, "../") {
		return ""
	}
	if _, err := a.sourcePath(clean); err != nil {
		return ""
	}
	return clean
}

func (a *app) sourcePath(relativePath string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid file path")
	}
	full := filepath.Join(a.repoRoot, clean)
	rel, err := filepath.Rel(a.repoRoot, full)
	if err != nil {
		return "", errors.New("invalid file path")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid file path")
	}
	return full, nil
}

func coverageTotals(fileCov *lineCoverage) (covered, executable int, percent float64) {
	executable = len(fileCov.Executable)
	covered = len(fileCov.Covered)
	if executable > 0 {
		percent = float64(covered) * 100.0 / float64(executable)
	}
	return covered, executable, percent
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		http.Error(w, "cannot encode JSON response", http.StatusInternalServerError)
	}
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>snapd Coverage Viewer</title>
  <style>
    :root {
      --bg: #f3f7f0;
      --ink: #222017;
      --ink-soft: #5c584c;
      --panel: rgba(255,255,255,0.82);
      --line: #d6dccb;
      --accent: #1d6f5f;
      --accent-soft: #ecf8f5;
      --ok: #1b8a42;
      --ok-soft: #dff6e5;
      --miss: #b13b2b;
      --miss-soft: #ffe8e4;
      --code: #f9fbf7;
      --shadow: 0 18px 40px rgba(33, 45, 24, 0.12);
      --radius: 14px;
    }

    * { box-sizing: border-box; }

    body {
      margin: 0;
      min-height: 100vh;
      font-family: "Avenir Next", "Fira Sans", "Segoe UI", sans-serif;
      color: var(--ink);
      background:
        radial-gradient(60rem 60rem at 20% -15%, #d6ead8 0%, transparent 45%),
        radial-gradient(55rem 55rem at 90% 110%, #d2e5ff 0%, transparent 40%),
        var(--bg);
      overflow: hidden;
    }

    .layout {
      display: grid;
      grid-template-columns: 320px minmax(520px, 1fr);
      gap: 16px;
      height: 100vh;
      padding: 16px;
      animation: rise 420ms ease-out;
    }

    .panel {
      background: var(--panel);
      border: 1px solid rgba(255,255,255,0.8);
      border-radius: var(--radius);
      box-shadow: var(--shadow);
      backdrop-filter: blur(5px);
    }

    .sidebar {
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .sidebar-header {
      padding: 18px 18px 10px;
      border-bottom: 1px solid var(--line);
    }

    h1 {
      margin: 0;
      font-size: 1.35rem;
      letter-spacing: 0.02em;
      font-weight: 700;
    }

    .muted {
      color: var(--ink-soft);
      margin-top: 6px;
      font-size: 0.92rem;
    }

    .controls {
      padding: 14px 18px;
      display: grid;
      gap: 10px;
      border-bottom: 1px solid var(--line);
    }

    label { font-weight: 600; font-size: 0.9rem; }

    select, input {
      width: 100%;
      border-radius: 10px;
      border: 1px solid #c8cfbc;
      background: #fff;
      color: var(--ink);
      padding: 10px 12px;
      font-size: 0.95rem;
      font-family: inherit;
    }

    .files {
      overflow: auto;
      padding: 10px;
      display: grid;
      gap: 8px;
    }

    .file-item {
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 10px;
      background: #fff;
      cursor: pointer;
      transition: transform 120ms ease, border-color 120ms ease;
      animation: stagger 280ms ease both;
    }

    .file-item:hover { transform: translateY(-1px); border-color: #aeb8a1; }
    .file-item.active { border-color: var(--accent); background: var(--accent-soft); }

    .file-path {
      font-family: "JetBrains Mono", "Fira Code", monospace;
      font-size: 0.78rem;
      overflow-wrap: anywhere;
    }

    .file-meta {
      margin-top: 6px;
      font-size: 0.82rem;
      color: var(--ink-soft);
      display: flex;
      justify-content: space-between;
    }

    .main {
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .main-header {
      padding: 14px 18px;
      border-bottom: 1px solid var(--line);
      display: flex;
      justify-content: space-between;
      align-items: baseline;
      gap: 12px;
    }

    .main-title {
      font-family: "JetBrains Mono", "Fira Code", monospace;
      font-size: 0.92rem;
      overflow-wrap: anywhere;
    }

    .badge {
      border-radius: 999px;
      padding: 4px 10px;
      background: #eef2e8;
      font-size: 0.82rem;
      white-space: nowrap;
    }

    .code-wrap {
      overflow: auto;
      padding: 10px 0 16px;
      background: var(--code);
      height: 100%;
    }

    .line {
      display: grid;
      grid-template-columns: 72px 1fr;
      align-items: start;
      font-family: "JetBrains Mono", "Fira Code", monospace;
      font-size: 0.84rem;
      line-height: 1.45;
      white-space: pre;
      border-left: 5px solid transparent;
    }

    .line.no { color: #7a7f72; text-align: right; padding: 0 10px; user-select: none; }
    .line.txt { padding-right: 16px; overflow-x: auto; }

    .covered { background: var(--ok-soft); border-left-color: var(--ok); }
    .uncovered { background: var(--miss-soft); border-left-color: var(--miss); }

    .empty {
      padding: 22px;
      color: var(--ink-soft);
      text-align: center;
      font-size: 0.96rem;
    }

    @media (max-width: 1100px) {
      body { overflow: auto; }
      .layout {
        height: auto;
        min-height: 100vh;
        grid-template-columns: 1fr;
      }
      .sidebar { max-height: 48vh; }
      .main { min-height: 56vh; }
    }

    @keyframes rise {
      from { transform: translateY(8px); opacity: 0; }
      to { transform: translateY(0); opacity: 1; }
    }

    @keyframes stagger {
      from { opacity: 0; transform: translateY(6px); }
      to { opacity: 1; transform: translateY(0); }
    }
  </style>
</head>
<body>
  <div class="layout">
    <aside class="panel sidebar">
      <div class="sidebar-header">
        <h1>Coverage by Test</h1>
        <div class="muted">Browse raw covdata artifacts and inspect line-level execution.</div>
      </div>
      <div class="controls">
        <div>
          <label for="testSelect">Test</label>
          <select id="testSelect"></select>
        </div>
        <div>
          <label for="fileFilter">Filter files</label>
          <input id="fileFilter" type="text" placeholder="type path fragment">
        </div>
      </div>
      <div id="fileList" class="files"></div>
    </aside>

    <main class="panel main">
      <div class="main-header">
        <div id="fileTitle" class="main-title">Select a test and a file</div>
        <div id="coverageBadge" class="badge">No file selected</div>
      </div>
      <div id="codeView" class="code-wrap">
        <div class="empty">Coverage lines appear here.</div>
      </div>
    </main>
  </div>

  <script>
    const testSelect = document.getElementById('testSelect');
    const fileFilter = document.getElementById('fileFilter');
    const fileList = document.getElementById('fileList');
    const fileTitle = document.getElementById('fileTitle');
    const coverageBadge = document.getElementById('coverageBadge');
    const codeView = document.getElementById('codeView');

    let files = [];
    let activeTest = '';
    let activeFile = '';

    function fmtPct(v) {
      return Number.isFinite(v) ? v.toFixed(1) + '%' : '0.0%';
    }

    function escapeHtml(s) {
      return s
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    async function loadTests() {
      const res = await fetch('/api/tests');
      if (!res.ok) throw new Error('Cannot load tests');
      const data = await res.json();
      testSelect.innerHTML = '';
      for (const t of data.tests) {
        const opt = document.createElement('option');
        opt.value = t;
        opt.textContent = t;
        testSelect.appendChild(opt);
      }
      if (data.tests.length > 0) {
        activeTest = data.tests[0];
        testSelect.value = activeTest;
        await loadFiles();
      } else {
        fileList.innerHTML = '<div class="empty">No test directories found in coverage-results.</div>';
      }
    }

    async function loadFiles() {
      activeFile = '';
      fileTitle.textContent = 'Select a file';
      coverageBadge.textContent = 'No file selected';
      codeView.innerHTML = '<div class="empty">Coverage lines appear here.</div>';

      const res = await fetch('/api/files?test=' + encodeURIComponent(activeTest));
      if (!res.ok) {
        fileList.innerHTML = '<div class="empty">Could not load files for selected test.</div>';
        return;
      }
      const data = await res.json();
      files = data.files || [];
      renderFileList();
    }

    function renderFileList() {
      const q = fileFilter.value.trim().toLowerCase();
      const shown = files.filter(f => f.path.toLowerCase().includes(q));
      if (shown.length === 0) {
        fileList.innerHTML = '<div class="empty">No files match this filter.</div>';
        return;
      }
      fileList.innerHTML = shown.map((f, idx) => {
        const active = f.path === activeFile ? 'active' : '';
				return '<div class="file-item ' + active + '" data-path="' + escapeHtml(f.path) + '" style="animation-delay:' + (idx * 14) + 'ms">'
					+ '<div class="file-path">' + escapeHtml(f.path) + '</div>'
					+ '<div class="file-meta">'
					+ '<span>' + f.covered + '/' + f.executable + ' lines</span>'
					+ '<span>' + fmtPct(f.percent) + '</span>'
					+ '</div>'
					+ '</div>';
      }).join('');

      for (const item of fileList.querySelectorAll('.file-item')) {
        item.addEventListener('click', () => selectFile(item.dataset.path));
      }
    }

    async function selectFile(path) {
      activeFile = path;
      renderFileList();
      const res = await fetch('/api/source?test=' + encodeURIComponent(activeTest) + '&file=' + encodeURIComponent(path));
      if (!res.ok) {
        fileTitle.textContent = path;
        coverageBadge.textContent = 'Unable to load source';
        codeView.innerHTML = '<div class="empty">Source file not found in workspace.</div>';
        return;
      }
      const data = await res.json();
      fileTitle.textContent = data.file;
			coverageBadge.textContent = data.covered + '/' + data.executable + ' executable lines - ' + fmtPct(data.percent);
      codeView.innerHTML = data.lines.map(line => {
        const css = line.status === 'covered' ? 'covered' : (line.status === 'uncovered' ? 'uncovered' : '');
				return '<div class="line ' + css + '">'
					+ '<div class="line no">' + line.number + '</div>'
					+ '<div class="line txt">' + escapeHtml(line.text || ' ') + '</div>'
					+ '</div>';
      }).join('');
      codeView.scrollTop = 0;
    }

    testSelect.addEventListener('change', async () => {
      activeTest = testSelect.value;
      await loadFiles();
    });

    fileFilter.addEventListener('input', renderFileList);

    loadTests().catch((err) => {
      fileList.innerHTML = '<div class="empty">' + escapeHtml(String(err)) + '</div>';
    });
  </script>
</body>
</html>
`

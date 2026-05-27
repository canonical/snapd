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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// data looks like <path>:<start-line>.<start-col>,<end-line>.<end-col> <num-statements> <count>
var profileLineRE = regexp.MustCompile(`^(.*):([0-9]+)\.[0-9]+,([0-9]+)\.[0-9]+\s+([0-9]+)\s+([0-9]+)$`)

type coveredLineSet map[int]struct{}

type coverageByFile map[string]coveredLineSet

type app struct {
	resultsDir string
	modulePath string
}

type functionsByFileJSON struct {
	Files []functionsByFileEntry `json:"files"`
}

type functionsByFileEntry struct {
	Path             string   `json:"path"`
	CoveredFunctions []string `json:"covered_functions"`
}

func main() {
	resultsDir := flag.String("results-dir", "", "path to directory containing raw coverage data")
	output := flag.String("output", "files", "output mode: files|functions")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s -results-dir <dir> [-output files|functions]\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Must be run from snapd code directory\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if strings.TrimSpace(*resultsDir) == "" {
		log.Fatalf("cannot continue: provide -results-dir")
	}

	if *output != "files" && *output != "functions" {
		log.Fatalf("cannot continue: invalid -output %q (expected files|functions)", *output)
	}

	absResultsDir, err := filepath.Abs(*resultsDir)
	if err != nil {
		log.Fatalf("cannot resolve results dir: %v", err)
	}
	if err := ensureDir(absResultsDir); err != nil {
		log.Fatalf("coverage results dir is not accessible: %v", err)
	}

	modulePath, err := readModulePath()
	if err != nil {
		log.Fatalf("could not find go.mod; utility must be called from snapd code directory: %v", err)
	}

	a := &app{
		resultsDir: absResultsDir,
		modulePath: modulePath,
	}

	if *output == "files" {
		if err := a.printCoveredFiles(); err != nil {
			log.Fatalf("cannot list covered files: %v", err)
		}
		return
	}

	if err := a.printFunctionsJSON(); err != nil {
		log.Fatalf("cannot print functions JSON: %v", err)
	}
}

func (a *app) printCoveredFiles() error {
	coverage, err := a.loadCoverage()
	if err != nil {
		return err
	}

	coveredFiles := make([]string, 0, len(coverage))
	for path, lines := range coverage {
		if len(lines) > 0 {
			coveredFiles = append(coveredFiles, path)
		}
	}
	sort.Strings(coveredFiles)

	for _, path := range coveredFiles {
		fmt.Println(path)
	}
	return nil
}

func (a *app) printFunctionsJSON() error {
	coverage, err := a.loadCoverage()
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(coverage))
	for path, lines := range coverage {
		if len(lines) > 0 {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	payload := functionsByFileJSON{Files: make([]functionsByFileEntry, 0, len(paths))}

	for _, path := range paths {
		covered, err := a.extractCoveredFunctions(path, coverage[path])
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

func (a *app) extractCoveredFunctions(filePath string, coveredLines coveredLineSet) ([]string, error) {
	sourcePath, err := filepath.Abs(filePath)
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
		if !hasCoveredLineInRange(coveredLines, start, end) {
			continue
		}
		coveredSet[funcDeclName(fset, fn)] = struct{}{}
	}

	return mapKeysSorted(coveredSet), nil
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

func funcDeclName(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := formatNode(fset, fn.Recv.List[0].Type)
	return fmt.Sprintf("(%s).%s", recv, fn.Name.Name)
}

func readModulePath() (string, error) {
	content, err := os.ReadFile("go.mod")
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

func (a *app) loadCoverage() (coverageByFile, error) {
	profilePath, err := a.runCovdataTextfmt(a.resultsDir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(profilePath)

	parsed, err := a.parseProfile(profilePath)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func (a *app) runCovdataTextfmt(covdataDir string) (string, error) {
	tmpFile, err := os.CreateTemp("", "snapd-cov-profile-*.out")
	if err != nil {
		return "", fmt.Errorf("cannot create temporary profile file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("cannot close temporary profile file: %v", err)
	}

	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i", covdataDir, "-o", tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("cannot convert raw coverage: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return tmpFile.Name(), nil
}

func (a *app) parseProfile(profilePath string) (coverageByFile, error) {
	file, err := os.Open(profilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open profile: %v", err)
	}
	defer file.Close()

	result := make(coverageByFile)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		matches := profileLineRE.FindStringSubmatch(line)
		if len(matches) != 6 {
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
		numStatements, err := strconv.Atoi(matches[4])
		if err != nil {
			continue
		}
		count, err := strconv.ParseInt(matches[5], 10, 64)
		if err != nil {
			continue
		}
		if endLine < startLine {
			startLine, endLine = endLine, startLine
		}

		lines := result[path]
		if lines == nil {
			lines = make(coveredLineSet)
			result[path] = lines
		}
		for ln := startLine; ln <= endLine; ln++ {
			if count > 0 && numStatements > 0 {
				lines[ln] = struct{}{}
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
	return clean
}

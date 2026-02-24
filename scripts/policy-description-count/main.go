package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type record struct {
	Policy      string
	File        string
	Path        string
	Description string
	CharCount   int
}

func main() {
	rootDir := flag.String("root", ".", "Repository root directory")
	outPath := flag.String("out", "", "Output CSV path (default: stdout)")
	flag.Parse()

	repoRoot, err := filepath.Abs(*rootDir)
	if err != nil {
		exitWithError(fmt.Errorf("resolve root path: %w", err))
	}

	policyFiles, err := findPolicyDefinitionFiles(repoRoot)
	if err != nil {
		exitWithError(err)
	}

	records := make([]record, 0, 256)
	for _, policyFile := range policyFiles {
		policyRecords, err := collectRecords(repoRoot, policyFile)
		if err != nil {
			exitWithError(err)
		}
		records = append(records, policyRecords...)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Policy != records[j].Policy {
			return records[i].Policy < records[j].Policy
		}
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].File < records[j].File
	})

	if err := writeCSV(records, *outPath); err != nil {
		exitWithError(err)
	}
}

func findPolicyDefinitionFiles(root string) ([]string, error) {
	policiesDir := filepath.Join(root, "policies")
	files := make([]string, 0, 64)

	err := filepath.WalkDir(policiesDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "policy-definition.yaml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk policies directory: %w", err)
	}
	return files, nil
}

func collectRecords(repoRoot, policyDefinitionPath string) ([]record, error) {
	content, err := os.ReadFile(policyDefinitionPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", policyDefinitionPath, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml %s: %w", policyDefinitionPath, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("unexpected yaml root in %s", policyDefinitionPath)
	}

	rootMap := doc.Content[0]

	policyName := scalarString(getMapValue(rootMap, "name"))
	if policyName == "" {
		policyName = filepath.Base(filepath.Dir(policyDefinitionPath))
	}

	relativePath, err := filepath.Rel(repoRoot, policyDefinitionPath)
	if err != nil {
		relativePath = policyDefinitionPath
	}

	records := make([]record, 0, 32)

	mainDescription := strings.TrimSpace(scalarString(getMapValue(rootMap, "description")))
	records = append(records, record{
		Policy:      policyName,
		File:        filepath.ToSlash(relativePath),
		Path:        "<policy.description>",
		Description: mainDescription,
		CharCount:   utf8.RuneCountInString(mainDescription),
	})

	parametersNode := getMapValue(rootMap, "parameters")
	if parametersNode != nil {
		collectParameterDescriptions(
			policyName,
			filepath.ToSlash(relativePath),
			parametersNode,
			"",
			&records,
		)
	}

	return records, nil
}

func collectParameterDescriptions(policyName, relativeFile string, schemaNode *yaml.Node, path string, records *[]record) {
	if schemaNode == nil || schemaNode.Kind != yaml.MappingNode {
		return
	}

	if path != "" {
		description := strings.TrimSpace(scalarString(getMapValue(schemaNode, "description")))
		*records = append(*records, record{
			Policy:      policyName,
			File:        relativeFile,
			Path:        path,
			Description: description,
			CharCount:   utf8.RuneCountInString(description),
		})
	}

	properties := getMapValue(schemaNode, "properties")
	if properties != nil && properties.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(properties.Content); i += 2 {
			propertyName := properties.Content[i].Value
			childNode := properties.Content[i+1]
			childPath := propertyName
			if path != "" {
				childPath = path + "." + propertyName
			}
			collectParameterDescriptions(policyName, relativeFile, childNode, childPath, records)
		}
	}

	items := getMapValue(schemaNode, "items")
	if items != nil {
		childPath := path + "[]"
		if path == "" {
			childPath = "[]"
		}
		collectParameterDescriptions(policyName, relativeFile, items, childPath, records)
	}
}

func getMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func scalarString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return ""
}

func writeCSV(records []record, outputPath string) error {
	var (
		writer io.Writer
		file   *os.File
		err    error
	)

	if outputPath == "" {
		writer = os.Stdout
	} else {
		file, err = os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create output csv %s: %w", outputPath, err)
		}
		defer func() {
			_ = file.Close()
		}()
		writer = file
	}

	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	if err := csvWriter.Write([]string{"policy", "file", "path", "description_letter_count", "description"}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for _, r := range records {
		if err := csvWriter.Write([]string{
			r.Policy,
			r.File,
			r.Path,
			fmt.Sprintf("%d", r.CharCount),
			r.Description,
		}); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}

	return csvWriter.Error()
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

package config

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

var configWarningPathPart = regexp.MustCompile(`([^.\[\]]+)|\[(\d+)\]`)

type configValueLocation struct {
	file string
	line int
}

func logConfigWarning(warning string) {
	key := strings.Fields(warning)
	if len(key) == 0 {
		log.Printf("Warning: %s", warning)
		return
	}
	location := findConfigValueLocation(key[0])
	if location.file == "" {
		log.Printf("Warning: invalid configuration setting %s (source line unavailable): %s", key[0], warning)
		return
	}
	log.Printf("Warning: %s:%d: %s: %s", location.file, location.line, key[0], warning)
}

func findConfigValueLocation(path string) configValueLocation {
	parts := make([]string, 0)
	for _, match := range configWarningPathPart.FindAllStringSubmatch(path, -1) {
		if match[1] != "" {
			parts = append(parts, match[1])
		} else {
			parts = append(parts, match[2])
		}
	}
	if len(parts) == 0 {
		return configValueLocation{}
	}
	configDir := paths.ConfigDir()
	files := []string{filepath.Join(configDir, "server.yml")}
	dropins, err := os.ReadDir(filepath.Join(configDir, "yaml.d"))
	if err == nil {
		for _, entry := range dropins {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || (ext != ".yml" && ext != ".yaml") || strings.HasSuffix(entry.Name(), ".bak") || strings.HasSuffix(entry.Name(), "~") || strings.HasSuffix(entry.Name(), ".orig") {
				continue
			}
			files = append(files, filepath.Join(configDir, "yaml.d", entry.Name()))
		}
	}
	files = append(files, filepath.Join(configDir, "secrets.yml"))
	var found configValueLocation
	for _, file := range files {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}
		var root yaml.Node
		if yaml.Unmarshal(data, &root) != nil || len(root.Content) == 0 {
			continue
		}
		if line := yamlNodePathLine(root.Content[0], parts); line > 0 {
			found = configValueLocation{file: filepath.Base(file), line: line}
		}
	}
	return found
}

func yamlNodePathLine(node *yaml.Node, parts []string) int {
	if len(parts) == 0 {
		return node.Line
	}
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == parts[0] {
				return yamlNodePathLine(node.Content[index+1], parts[1:])
			}
		}
	case yaml.SequenceNode:
		index, err := strconv.Atoi(parts[0])
		if err == nil && index >= 0 && index < len(node.Content) {
			return yamlNodePathLine(node.Content[index], parts[1:])
		}
	}
	return 0
}

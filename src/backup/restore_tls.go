package backup

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func externalTLSConfigPaths(roots []string, files []ManifestFile) map[string]string {
	var configDir string
	for _, root := range roots {
		if filepath.Base(filepath.Clean(root)) == "server.yml" {
			configDir = filepath.Dir(root)
			break
		}
	}
	if configDir == "" {
		return nil
	}
	paths := make(map[string]string)
	for _, file := range files {
		switch filepath.ToSlash(file.Path) {
		case "ssl-external/cert.pem":
			paths["cert"] = filepath.Join(configDir, "ssl", "cert.pem")
		case "ssl-external/key.pem":
			paths["key"] = filepath.Join(configDir, "ssl", "key.pem")
		}
	}
	return paths
}

func rewriteRestoredTLSConfig(path string, external map[string]string, ensure bool) error {
	if len(external) == 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("rewrite restored TLS paths: invalid YAML")
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		if !ensure {
			return nil
		}
		document.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := document.Content[0]
	server := mappingValue(root, "server")
	if server == nil && ensure {
		server = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendMappingValue(root, "server", server)
	}
	if server == nil || server.Kind != yaml.MappingNode {
		return nil
	}
	ssl := mappingValue(server, "ssl")
	if ssl == nil && ensure {
		ssl = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendMappingValue(server, "ssl", ssl)
	}
	if ssl == nil || ssl.Kind != yaml.MappingNode {
		return nil
	}
	for key, value := range external {
		if valueNode := mappingValue(ssl, key); valueNode != nil {
			valueNode.Kind = yaml.ScalarNode
			valueNode.Tag = "!!str"
			valueNode.Value = value
		} else if ensure {
			appendMappingValue(ssl, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		}
	}
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, updated, info.Mode().Perm()); err != nil {
		reportCriticalWriteError(err)
		return err
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func appendMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

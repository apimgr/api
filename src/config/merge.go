package config

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/apimgr/api/src/paths"
	"gopkg.in/yaml.v3"
)

var lastGoodConfigFiles = map[string]map[string]interface{}{}
var configFileCacheMu sync.Mutex

var secretConfigPaths = []string{
	"server.token",
	"server.security.encryption_key",
	"server.security.encryption_key_version",
	"server.notifications.email.smtp.password",
	"server.cache.password",
	"server.database.token",
	"server.database.url",
	"server.cache.url",
	"server.pages.contact.captcha_secret_key",
	"server.ssl.letsencrypt.dns_credentials",
	"server.backup.encryption_password",
	"server.metrics.auth.tokens.prometheus",
	"server.metrics.auth.tokens.grafana",
	"server.metrics.auth.tokens.loki",
	"data.cve.api_key",
}

func loadMergedConfig(defaults *Config) ([]byte, error) {
	defaultData, err := yaml.Marshal(defaults)
	if err != nil {
		return nil, fmt.Errorf("marshal configuration defaults: %w", err)
	}
	var defaultMap map[string]interface{}
	if err := yaml.Unmarshal(defaultData, &defaultMap); err != nil {
		return nil, fmt.Errorf("decode configuration defaults: %w", err)
	}
	merged := cloneConfigMap(defaultMap)

	serverPath := filepath.Join(paths.ConfigDir(), "server.yml")
	if values, ok := readConfigFile(serverPath, true, false); ok {
		merged = mergeConfigMap(merged, defaultMap, values)
	}

	dropDir := filepath.Join(paths.ConfigDir(), "yaml.d")
	if err := prepareDropInDirectory(dropDir); err != nil {
		log.Printf("Warning: cannot read config drop-in directory %s: %v", dropDir, err)
	} else if entries, err := os.ReadDir(dropDir); err == nil {
		files := make([]string, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".orig") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".yml" && ext != ".yaml" {
				continue
			}
			files = append(files, filepath.Join(dropDir, name))
		}
		sort.Slice(files, func(i, j int) bool {
			return filepath.Base(files[i]) < filepath.Base(files[j])
		})
		for _, path := range files {
			if values, ok := readConfigFile(path, false, true); ok {
				log.Printf("Config: loaded drop-in %s", filepath.Base(path))
				merged = mergeConfigMap(merged, defaultMap, values)
			}
		}
	} else if !os.IsNotExist(err) {
		log.Printf("Warning: cannot list config drop-in directory %s: %v", dropDir, err)
	}

	secretsPath := filepath.Join(paths.ConfigDir(), "secrets.yml")
	if values, ok := readConfigFile(secretsPath, true, false); ok {
		merged = mergeConfigMap(merged, defaultMap, values)
	}
	if changed, err := migrateConfigFiles(defaultMap, merged); err != nil {
		log.Printf("Warning: configuration migration skipped: %v", err)
	} else if changed {
		return loadMergedConfig(defaults)
	}
	if err := protectDNSCredentials(merged); err != nil {
		return nil, err
	}
	return yaml.Marshal(merged)
}

// protectDNSCredentials keeps DNS provider values encrypted in secrets.yml,
// while returning the decrypted map to the in-memory configuration decoder.
func protectDNSCredentials(merged map[string]interface{}) error {
	const credentialsPath = "server.ssl.letsencrypt.dns_credentials"
	value, ok := getConfigPath(merged, credentialsPath)
	if !ok || value == nil {
		return nil
	}
	credentials, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("%s must be a flat map of strings", credentialsPath)
	}
	keyValue, _ := getConfigPath(merged, "server.security.encryption_key")
	key, _ := keyValue.(string)
	needsKey := false
	for name, raw := range credentials {
		credential, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%s.%s must be a string", credentialsPath, name)
		}
		if strings.HasPrefix(credential, dnsCredentialCipherPrefix) && key == "" {
			return fmt.Errorf("cannot decrypt DNS credential %s without server.security.encryption_key", name)
		}
		if !strings.HasPrefix(credential, dnsCredentialCipherPrefix) && credential != "" {
			needsKey = true
		}
	}
	if key == "" && needsKey {
		generated, err := randomSecretString(encryptionKeyBytes, true)
		if err != nil {
			return fmt.Errorf("generate encryption key for DNS credentials: %w", err)
		}
		key = generated
		setConfigPath(merged, "server.security.encryption_key", key)
		setConfigPath(merged, "server.security.encryption_key_version", 1)
	}
	securityValue, _ := getConfigPath(merged, "server.security")
	security, _ := securityValue.(map[string]interface{})
	previous, _ := security["encryption_key_previous"].(string)
	for name, raw := range credentials {
		credential := raw.(string)
		plain, err := DecryptDNSCredential(credential, key, previous)
		if err != nil {
			return fmt.Errorf("cannot decrypt DNS credential %s", name)
		}
		credentials[name] = plain
	}
	if !needsKey {
		return nil
	}
	// Persist through the existing atomic, owner-only config writer. Migration
	// has already moved all credential sources into the canonical secret file.
	configDir := paths.ConfigDir()
	secretPath := filepath.Join(configDir, "secrets.yml")
	if info, err := os.Lstat(secretPath); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("secrets.yml must be a regular file for DNS credential encryption")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	secretMap, _ := readConfigFile(secretPath, false, false)
	if secretMap == nil {
		secretMap = map[string]interface{}{}
	}
	encrypted := make(map[string]interface{}, len(credentials))
	for name, raw := range credentials {
		plain := raw.(string)
		sealed, err := EncryptDNSCredential(plain, key)
		if err != nil {
			return fmt.Errorf("encrypt DNS credential %s", name)
		}
		encrypted[name] = sealed
	}
	setConfigPath(secretMap, credentialsPath, encrypted)
	setConfigPath(secretMap, "server.security.encryption_key", key)
	if version, found := getConfigPath(merged, "server.security.encryption_key_version"); found {
		setConfigPath(secretMap, "server.security.encryption_key_version", version)
	}
	secretData, err := yaml.Marshal(secretMap)
	if err != nil {
		return err
	}
	if err := writeConfigAtomically(secretPath, secretData, 0600); err != nil {
		return err
	}
	return nil
}

// migrateConfigFiles moves legacy server.yml sections and hand-written
// credentials into their canonical files without changing effective values.
func migrateConfigFiles(defaults, merged map[string]interface{}) (bool, error) {
	configDir := paths.ConfigDir()
	serverPath := filepath.Join(configDir, "server.yml")
	server, ok := readConfigFile(serverPath, false, false)
	serverExists := ok
	if !ok {
		if _, err := os.Lstat(serverPath); !os.IsNotExist(err) {
			return false, nil
		}
		server = map[string]interface{}{}
	}
	var original []byte
	if serverExists {
		var err error
		original, err = os.ReadFile(serverPath)
		if err != nil {
			return false, err
		}
		var parsed map[string]interface{}
		if err := yaml.Unmarshal(original, &parsed); err != nil {
			return false, nil
		}
	}
	changed := false
	serverCopy := cloneConfigMap(server)
	areaFiles := map[string]map[string]interface{}{}
	knownServerKeys := map[string]bool{
		"port": true, "address": true, "fqdn": true, "mode": true,
		"api_version": true, "baseurl": true, "pidfile": true,
		"daemonize": true, "debug": true, "database": true,
		"cache": true, "ssl": true, "trusted_proxies": true,
	}
	serverSection, _ := serverCopy["server"].(map[string]interface{})
	for key, value := range serverSection {
		filename := areaFileForServerKey(key)
		if filename == "" || knownServerKeys[key] {
			continue
		}
		if areaFiles[filename] == nil {
			areaFiles[filename] = map[string]interface{}{}
		}
		setConfigPath(areaFiles[filename], "server."+key, value)
		delete(serverSection, key)
		changed = true
	}
	if len(serverSection) == 0 {
		delete(serverCopy, "server")
	}
	for key, value := range serverCopy {
		if key == "server" {
			continue
		}
		if key == "cli" {
			if areaFiles["maintenance.yml"] == nil {
				areaFiles["maintenance.yml"] = map[string]interface{}{}
			}
			areaFiles["maintenance.yml"]["cli"] = cloneConfigValue(value)
			delete(serverCopy, key)
			changed = true
			continue
		}
		if key == "tor" || key == "i2p" {
			filename := key + ".yml"
			if areaFiles[filename] == nil {
				areaFiles[filename] = map[string]interface{}{}
			}
			areaFiles[filename][key] = cloneConfigValue(value)
			delete(serverCopy, key)
			changed = true
			continue
		}
		if key == "data" {
			if areaFiles["security.yml"] == nil {
				areaFiles["security.yml"] = map[string]interface{}{}
			}
			areaFiles["security.yml"][key] = cloneConfigValue(value)
			delete(serverCopy, key)
			changed = true
			continue
		}
		if key == "web" {
			web, _ := value.(map[string]interface{})
			if ui, exists := web["ui"]; exists {
				if areaFiles["branding.yml"] == nil {
					areaFiles["branding.yml"] = map[string]interface{}{}
				}
				setConfigPath(areaFiles["branding.yml"], "web.ui", ui)
				delete(web, "ui")
				changed = true
			}
			if len(web) > 0 {
				if areaFiles["web.yml"] == nil {
					areaFiles["web.yml"] = map[string]interface{}{}
				}
				areaFiles["web.yml"]["web"] = cloneConfigMap(web)
				delete(serverCopy, "web")
				changed = true
			}
			continue
		}
		if areaFiles["app.yml"] == nil {
			areaFiles["app.yml"] = map[string]interface{}{}
		}
		areaFiles["app.yml"][key] = cloneConfigValue(value)
		delete(serverCopy, key)
		changed = true
	}

	secrets := map[string]interface{}{}
	writeSecrets := false
	secretsPath := filepath.Join(configDir, "secrets.yml")
	if info, err := os.Lstat(secretsPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
		raw, readErr := os.ReadFile(secretsPath)
		var parsed map[string]interface{}
		if readErr != nil || yaml.Unmarshal(raw, &parsed) != nil {
			return false, nil
		}
	} else if !os.IsNotExist(err) {
		return false, nil
	}
	if existing, ok := readConfigFile(secretsPath, false, false); ok {
		secrets = cloneConfigMap(existing)
	}
	for _, path := range secretConfigPaths {
		value, found := getConfigPath(merged, path)
		if !found || !secretValuePresent(value) {
			continue
		}
		if path == "server.database.url" || path == "server.cache.url" {
			text, ok := value.(string)
			if !ok || !urlHasCredentials(text) {
				continue
			}
		}
		if _, exists := getConfigPath(secrets, path); !exists {
			setConfigPath(secrets, path, value)
			writeSecrets = true
			changed = true
		}
	}
	if removeSecretPaths(serverCopy) {
		changed = true
	}
	dropDir := filepath.Join(configDir, "yaml.d")
	changedDropIns := map[string]map[string]interface{}{}
	if entries, err := os.ReadDir(dropDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if entry.IsDir() || (ext != ".yml" && ext != ".yaml") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".orig") {
				continue
			}
			path := filepath.Join(dropDir, name)
			values, ok := readConfigFile(path, false, true)
			if !ok {
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var parsed map[string]interface{}
			if err := yaml.Unmarshal(raw, &parsed); err != nil {
				continue
			}
			if !removeSecretPaths(values) {
				continue
			}
			changedDropIns[path] = values
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Join(configDir, "yaml.d"), 0700); err != nil {
		return false, err
	}
	if err := os.Chmod(filepath.Join(configDir, "yaml.d"), 0700); err != nil {
		return false, err
	}
	if serverExists {
		if err := writeConfigAtomically(serverPath+".bak", original, 0600); err != nil {
			return false, err
		}
	}
	if writeSecrets {
		secretData, err := yaml.Marshal(secrets)
		if err != nil {
			return false, err
		}
		if err := writeConfigAtomically(secretsPath, secretData, 0600); err != nil {
			return false, err
		}
	}
	for path, values := range changedDropIns {
		data, err := yaml.Marshal(values)
		if err != nil {
			return false, err
		}
		if err := writeConfigAtomically(path, data, 0600); err != nil {
			return false, err
		}
	}
	for name, moved := range areaFiles {
		path := filepath.Join(configDir, "yaml.d", name)
		if existing, ok := readConfigFile(path, false, true); ok {
			moved = mergeConfigMap(moved, defaults, existing)
		}
		data, err := yaml.Marshal(moved)
		if err != nil {
			return false, err
		}
		if err := writeConfigAtomically(path, data, 0600); err != nil {
			return false, err
		}
	}
	data, err := yaml.Marshal(serverCopy)
	if err != nil {
		return false, err
	}
	text := "# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n\n" + string(data)
	if err := writeConfigAtomically(serverPath, []byte(text), 0600); err != nil {
		return false, err
	}
	return true, nil
}

func areaFileForServerKey(key string) string {
	switch key {
	case "branding", "seo":
		return "branding.yml"
	case "healthz", "csrf", "i18n", "cors":
		return "web.yml"
	case "pages":
		return "pages.yml"
	case "contact":
		return "contact.yml"
	case "privacy", "compliance", "tracking":
		return "privacy.yml"
	case "security", "rate_limit", "geoip", "tokens", "limits":
		return "security.yml"
	case "logs":
		return "logs.yml"
	case "metrics":
		return "metrics.yml"
	case "notifications":
		return "notifications.yml"
	case "tor":
		return "tor.yml"
	case "i2p":
		return "i2p.yml"
	case "scheduler", "update", "backup":
		return "maintenance.yml"
	default:
		return "app.yml"
	}
}

func removeSecretPaths(root map[string]interface{}) bool {
	changed := false
	for _, path := range secretConfigPaths {
		if path == "server.database.url" || path == "server.cache.url" {
			value, ok := getConfigPath(root, path)
			text, isString := value.(string)
			if !ok || !isString || !urlHasCredentials(text) {
				continue
			}
		}
		if _, ok := deleteConfigPath(root, path); ok {
			changed = true
		}
	}
	return changed
}

func saveConfig(cfg *Config) error {
	configDir := paths.ConfigDir()
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(configDir, 0700); err != nil {
		return fmt.Errorf("set config directory mode 0700: %w", err)
	}
	desiredData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	var desired map[string]interface{}
	if err := yaml.Unmarshal(desiredData, &desired); err != nil {
		return err
	}
	currentData, err := loadMergedConfig(defaultConfig())
	if err != nil {
		return err
	}
	var current map[string]interface{}
	if err := yaml.Unmarshal(currentData, &current); err != nil {
		return err
	}
	changes := configChanges(current, desired, "")
	oldKey, _ := getConfigPath(current, "server.security.encryption_key")
	newKey, _ := getConfigPath(desired, "server.security.encryption_key")
	if oldKey != newKey {
		if credentialMap, ok := getConfigPath(desired, "server.ssl.letsencrypt.dns_credentials"); ok {
			if credentials, ok := credentialMap.(map[string]interface{}); ok {
				for name := range credentials {
					changes = append(changes, "server.ssl.letsencrypt.dns_credentials."+name)
				}
			}
		}
	}
	files := map[string]map[string]interface{}{}
	targetFiles := map[string]bool{}
	loadForUpdate := func(path string) map[string]interface{} {
		if values, exists := files[path]; exists {
			return values
		}
		values, _ := readConfigFile(path, false, filepath.Dir(path) == filepath.Join(configDir, "yaml.d"))
		if values == nil {
			values = map[string]interface{}{}
		}
		files[path] = values
		return values
	}
	for _, keyPath := range changes {
		value, hasValue := getConfigPath(desired, keyPath)
		secret := isSecretConfigPath(keyPath, value, hasValue)
		target := filepath.Join(configDir, "server.yml")
		if secret {
			target = filepath.Join(configDir, "secrets.yml")
		} else if area := areaFileForConfigPath(keyPath); area != "" {
			target = filepath.Join(configDir, "yaml.d", area)
		}
		targetFiles[target] = true
		for _, sourcePath := range configSourceFiles(configDir) {
			if sourcePath == target {
				continue
			}
			if sourcePath == filepath.Join(configDir, "secrets.yml") && !secret && keyPath != "server.database.url" && keyPath != "server.cache.url" {
				continue
			}
			_, existed := getConfigPath(loadForUpdate(sourcePath), keyPath)
			if existed {
				deleteConfigPath(files[sourcePath], keyPath)
			}
		}
		values := loadForUpdate(target)
		if hasValue {
			if strings.HasPrefix(keyPath, "server.ssl.letsencrypt.dns_credentials.") {
				credential, ok := value.(string)
				if !ok {
					return fmt.Errorf("%s must be a string", keyPath)
				}
				keyValue, _ := getConfigPath(desired, "server.security.encryption_key")
				key, _ := keyValue.(string)
				sealed, err := EncryptDNSCredential(credential, key)
				if err != nil {
					return fmt.Errorf("encrypt DNS credential %s", strings.TrimPrefix(keyPath, "server.ssl.letsencrypt.dns_credentials."))
				}
				value = sealed
			}
			setConfigPath(values, keyPath, value)
		} else {
			deleteConfigPath(values, keyPath)
		}
	}
	pathsToWrite := make([]string, 0, len(files))
	for path := range files {
		pathsToWrite = append(pathsToWrite, path)
	}
	sort.Strings(pathsToWrite)
	write := func(path string) error {
		values := files[path]
		if filepath.Dir(path) == filepath.Join(configDir, "yaml.d") {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return err
			}
			if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
				return err
			}
		}
		data, err := yaml.Marshal(values)
		if err != nil {
			return err
		}
		if path == filepath.Join(configDir, "server.yml") {
			data = append([]byte("# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n\n"), data...)
		}
		if err := writeConfigAtomically(path, data, 0600); err != nil {
			return err
		}
		return nil
	}
	for _, priority := range []bool{true, false} {
		for _, path := range pathsToWrite {
			if targetFiles[path] != priority {
				continue
			}
			if err := write(path); err != nil {
				return err
			}
		}
	}
	serverPath := filepath.Join(configDir, "server.yml")
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		if err := writeConfigAtomically(serverPath, []byte("# This node only: listen, identity, database, cache, ssl. The rest is in yaml.d/ and credentials are in secrets.yml.\n\n{}\n"), 0600); err != nil {
			return err
		}
	}
	return nil
}

func configChanges(old, current map[string]interface{}, prefix string) []string {
	keys := make(map[string]struct{}, len(old)+len(current))
	for key := range old {
		keys[key] = struct{}{}
	}
	for key := range current {
		keys[key] = struct{}{}
	}
	var changes []string
	for key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		oldValue, oldOK := old[key]
		newValue, newOK := current[key]
		oldMap, oldIsMap := oldValue.(map[string]interface{})
		newMap, newIsMap := newValue.(map[string]interface{})
		if oldIsMap || newIsMap {
			if !oldIsMap {
				oldMap = map[string]interface{}{}
			}
			if !newIsMap {
				newMap = map[string]interface{}{}
			}
			changes = append(changes, configChanges(oldMap, newMap, path)...)
			continue
		}
		if oldOK != newOK || !configValuesEqual(oldValue, newValue) {
			changes = append(changes, path)
		}
	}
	return changes
}

func configValuesEqual(a, b interface{}) bool {
	left, errLeft := yaml.Marshal(a)
	right, errRight := yaml.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func configSourceFiles(configDir string) []string {
	files := []string{filepath.Join(configDir, "server.yml"), filepath.Join(configDir, "secrets.yml")}
	dir := filepath.Join(configDir, "yaml.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".orig") || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files
}

func isSecretConfigPath(path string, value interface{}, exists bool) bool {
	if strings.HasPrefix(path, "server.ssl.letsencrypt.dns_credentials.") {
		return true
	}
	if strings.HasPrefix(path, "server.contact.") {
		parts := strings.Split(path, ".")
		if len(parts) == 5 && parts[3] == "webhooks" && parts[4] != "" {
			return true
		}
	}
	for _, secretPath := range secretConfigPaths {
		if path == secretPath && path != "server.database.url" && path != "server.cache.url" {
			return true
		}
	}
	if (path == "server.database.url" || path == "server.cache.url") && exists {
		text, ok := value.(string)
		return ok && urlHasCredentials(text)
	}
	return false
}

func areaFileForConfigPath(path string) string {
	if strings.HasPrefix(path, "cli.") || path == "cli" {
		return "maintenance.yml"
	}
	if strings.HasPrefix(path, "server.") {
		parts := strings.Split(path, ".")
		if len(parts) < 2 {
			return "app.yml"
		}
		if parts[1] == "database" || parts[1] == "cache" || parts[1] == "ssl" || parts[1] == "trusted_proxies" || map[string]bool{"port": true, "address": true, "fqdn": true, "mode": true, "api_version": true, "baseurl": true, "pidfile": true, "daemonize": true, "debug": true}[parts[1]] {
			return ""
		}
		return areaFileForServerKey(parts[1])
	}
	if path == "web.ui" || strings.HasPrefix(path, "web.ui.") {
		return "branding.yml"
	}
	if strings.HasPrefix(path, "web.") {
		return "web.yml"
	}
	if strings.HasPrefix(path, "data.") {
		return "security.yml"
	}
	return "app.yml"
}

func urlHasCredentials(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.User != nil {
		return true
	}
	query := parsed.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "password", "pwd", "token", "authtoken":
			return true
		}
	}
	return false
}

func secretValuePresent(value interface{}) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return true
}

func getConfigPath(root map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = root
	for _, part := range parts {
		child, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = child[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func deleteConfigPath(root map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		child, ok := current[part].(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = child
	}
	value, ok := current[parts[len(parts)-1]]
	delete(current, parts[len(parts)-1])
	return value, ok
}

func setConfigPath(root map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		child, ok := current[part].(map[string]interface{})
		if !ok {
			child = make(map[string]interface{})
			current[part] = child
		}
		current = child
	}
	current[parts[len(parts)-1]] = cloneConfigValue(value)
}

func writeConfigAtomically(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() {
		if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove temporary config file %s: %v", filepath.Base(tmp), err)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return errors.Join(err, file.Close())
	}
	if _, err := file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	return atomicConfigReplace(tmp, path)
}

func prepareDropInDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("path is not a directory")
	}
	if info.Mode().Perm() != 0700 {
		if err := os.Chmod(dir, 0700); err != nil {
			return fmt.Errorf("set directory mode 0700: %w", err)
		}
	}
	return nil
}

func readConfigFile(path string, required, allowContainedSymlink bool) (map[string]interface{}, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) && required {
			log.Printf("Warning: cannot stat config file %s: %v", path, err)
		}
		return nil, false
	}
	resolved := path
	if info.Mode()&os.ModeSymlink != 0 {
		if !allowContainedSymlink {
			log.Printf("Warning: ignoring config symlink %s: symlinks are not allowed here", filepath.Base(path))
			return nil, false
		}
		resolved, err = filepath.EvalSymlinks(path)
		if err != nil {
			log.Printf("Warning: ignoring config file %s: %v", filepath.Base(path), err)
			return nil, false
		}
		resolvedDir, dirErr := filepath.EvalSymlinks(filepath.Dir(path))
		relative, relErr := filepath.Rel(resolvedDir, resolved)
		if dirErr != nil || relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			log.Printf("Warning: ignoring config symlink %s: target must stay inside yaml.d", filepath.Base(path))
			return nil, false
		}
		info, err = os.Stat(resolved)
	}
	if err != nil || !info.Mode().IsRegular() {
		log.Printf("Warning: ignoring config file %s: not a regular file", filepath.Base(path))
		return nil, false
	}
	if info.Mode().Perm() != 0600 {
		if err := os.Chmod(resolved, 0600); err != nil {
			log.Printf("Warning: ignoring config file %s: cannot set mode 0600: %v", filepath.Base(path), err)
			return nil, false
		}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		log.Printf("Warning: ignoring config file %s: %v", filepath.Base(path), err)
		return nil, false
	}
	values := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &values); err != nil {
		configFileCacheMu.Lock()
		cached, ok := lastGoodConfigFiles[path]
		configFileCacheMu.Unlock()
		if ok {
			log.Printf("Warning: config file %s cannot be parsed; retaining last good values: %v", filepath.Base(path), err)
			return cloneConfigMap(cached), true
		}
		log.Printf("Warning: ignoring config file %s because it cannot be parsed: %v", filepath.Base(path), err)
		return nil, false
	}
	configFileCacheMu.Lock()
	lastGoodConfigFiles[path] = cloneConfigMap(values)
	configFileCacheMu.Unlock()
	return values, true
}

func mergeConfigMap(base, defaults, overlay map[string]interface{}) map[string]interface{} {
	result := cloneConfigMap(base)
	for key, value := range overlay {
		if value == nil {
			if defaultValue, ok := defaults[key]; ok {
				result[key] = cloneConfigValue(defaultValue)
			} else {
				delete(result, key)
			}
			continue
		}
		overlayMap, isMap := value.(map[string]interface{})
		if !isMap {
			result[key] = cloneConfigValue(value)
			continue
		}
		baseChild, _ := result[key].(map[string]interface{})
		defaultChild, _ := defaults[key].(map[string]interface{})
		result[key] = mergeConfigMap(baseChild, defaultChild, overlayMap)
	}
	return result
}

// unmarshalConfigResilient applies valid values individually when a single
// invalid YAML type would otherwise reject the merged configuration.
func unmarshalConfigResilient(data []byte, cfg *Config) error {
	if err := yaml.Unmarshal(data, cfg); err == nil {
		return nil
	}
	var merged map[string]interface{}
	if err := yaml.Unmarshal(data, &merged); err != nil {
		return err
	}
	defaultsData, err := yaml.Marshal(defaultConfig())
	if err != nil {
		return err
	}
	var defaults map[string]interface{}
	if err := yaml.Unmarshal(defaultsData, &defaults); err != nil {
		return err
	}
	paths := make([]string, 0)
	collectConfigLeaves(merged, "", &paths)
	sort.Strings(paths)
	candidate := cloneConfigMap(defaults)
	for _, keyPath := range paths {
		value, ok := getConfigPath(merged, keyPath)
		if !ok {
			continue
		}
		trialMap := cloneConfigMap(candidate)
		setConfigPath(trialMap, keyPath, value)
		trialData, err := yaml.Marshal(trialMap)
		if err != nil {
			return err
		}
		trial := defaultConfig()
		if err := yaml.Unmarshal(trialData, trial); err != nil {
			log.Printf("Warning: invalid config value for %s; using its default", keyPath)
			continue
		}
		candidate = trialMap
	}
	finalData, err := yaml.Marshal(candidate)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(finalData, cfg)
}

func collectConfigLeaves(values map[string]interface{}, prefix string, result *[]string) {
	for key, value := range values {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if nested, ok := value.(map[string]interface{}); ok && len(nested) > 0 {
			collectConfigLeaves(nested, path, result)
			continue
		}
		*result = append(*result, path)
	}
}

func cloneConfigMap(value map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(value))
	for key, item := range value {
		copy[key] = cloneConfigValue(item)
	}
	return copy
}

func cloneConfigValue(value interface{}) interface{} {
	switch item := value.(type) {
	case map[string]interface{}:
		return cloneConfigMap(item)
	case []interface{}:
		copy := make([]interface{}, len(item))
		for i := range item {
			copy[i] = cloneConfigValue(item[i])
		}
		return copy
	default:
		return value
	}
}

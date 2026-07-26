package generate

import (
	"fmt"
	"strings"
)

// SQLColumn describes a single column in a CREATE TABLE request.
type SQLColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	PrimaryKey bool   `json:"primary_key"`
	NotNull    bool   `json:"not_null"`
	Unique     bool   `json:"unique"`
}

// sqlTypeMap maps portable logical column types to standards-compliant SQL
// types. Scope is limited to CREATE TABLE generation only.
var sqlTypeMap = map[string]string{
	"integer":   "INTEGER",
	"int":       "INTEGER",
	"string":    "VARCHAR(255)",
	"varchar":   "VARCHAR(255)",
	"text":      "TEXT",
	"boolean":   "BOOLEAN",
	"bool":      "BOOLEAN",
	"timestamp": "TIMESTAMP",
	"datetime":  "TIMESTAMP",
	"float":     "DOUBLE PRECISION",
	"double":    "DOUBLE PRECISION",
}

// SQL renders a CREATE TABLE statement for the given table name and column
// definitions.
func (s *Service) SQL(table string, columns []SQLColumn) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return "", fmt.Errorf("table name is required")
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("at least one column is required")
	}

	var lines []string
	var primaryKeys []string

	for _, col := range columns {
		name := strings.TrimSpace(col.Name)
		if name == "" {
			return "", fmt.Errorf("column name must not be empty")
		}
		sqlType, ok := sqlTypeMap[strings.ToLower(strings.TrimSpace(col.Type))]
		if !ok {
			return "", fmt.Errorf("unsupported column type %q for column %q: supported types are integer, string, text, boolean, timestamp, float", col.Type, name)
		}

		line := fmt.Sprintf("  %s %s", name, sqlType)
		if col.NotNull || col.PrimaryKey {
			line += " NOT NULL"
		}
		if col.Unique {
			line += " UNIQUE"
		}
		lines = append(lines, line)

		if col.PrimaryKey {
			primaryKeys = append(primaryKeys, name)
		}
	}

	if len(primaryKeys) > 0 {
		lines = append(lines, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(primaryKeys, ", ")))
	}

	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);\n", table, strings.Join(lines, ",\n")), nil
}

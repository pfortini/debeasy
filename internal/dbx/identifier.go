package dbx

import (
	"fmt"
	"strings"
)

// quoteIdent quotes a SQL identifier for the given dialect. It rejects identifiers
// that contain the dialect's quote char (no embedded quote handling — fail closed).
func quoteIdent(k Kind, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	switch k {
	case KindMySQL:
		if strings.ContainsRune(name, '`') {
			return "", fmt.Errorf("identifier may not contain backtick: %q", name)
		}
		return "`" + name + "`", nil
	default: // postgres, sqlite — both use double-quote
		if strings.ContainsRune(name, '"') {
			return "", fmt.Errorf("identifier may not contain double-quote: %q", name)
		}
		return `"` + name + `"`, nil
	}
}

func mustQuoteIdent(k Kind, name string) string {
	q, err := quoteIdent(k, name)
	if err != nil {
		// callers should validate; this is a safety net to avoid SQL injection
		panic(err)
	}
	return q
}

func qualified(k Kind, schema, name string) (string, error) {
	if schema == "" {
		return quoteIdent(k, name)
	}
	s, err := quoteIdent(k, schema)
	if err != nil {
		return "", err
	}
	n, err := quoteIdent(k, name)
	if err != nil {
		return "", err
	}
	return s + "." + n, nil
}

// quoteLiteral safely escapes a string literal (single quotes doubled). Used only for places
// where parameter binding is impossible (e.g. CREATE DATABASE name).
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

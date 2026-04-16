package dbx

import (
	"fmt"
	"strings"
)

// buildCreateTable builds a CREATE TABLE statement for the given dialect with safely quoted
// identifiers. Data types are passed through verbatim — caller is responsible for choosing
// types valid in the dialect.
func buildCreateTable(k Kind, def CreateTableDef) (string, error) {
	if len(def.Columns) == 0 {
		return "", fmt.Errorf("at least one column required")
	}
	tbl, err := qualified(k, def.Schema, def.Name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("CREATE TABLE ")
	if def.IfNotExists {
		b.WriteString("IF NOT EXISTS ")
	}
	b.WriteString(tbl)
	b.WriteString(" (\n")

	pks := []string{}
	for i, c := range def.Columns {
		col, err := quoteIdent(k, c.Name)
		if err != nil {
			return "", err
		}
		dt := strings.TrimSpace(c.DataType)
		if dt == "" {
			return "", fmt.Errorf("column %q missing data type", c.Name)
		}
		line := "  " + col + " " + dt
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.Default != "" {
			line += " DEFAULT " + c.Default
		}
		if c.IsPK {
			pks = append(pks, col)
		}
		if i < len(def.Columns)-1 || len(pks) > 0 {
			line += ","
		}
		b.WriteString(line + "\n")
	}
	if len(pks) > 0 {
		b.WriteString("  PRIMARY KEY (" + strings.Join(pks, ", ") + ")\n")
	}
	b.WriteString(")")
	return b.String(), nil
}

func buildCreateIndex(k Kind, def IndexDef) (string, error) {
	if len(def.Columns) == 0 {
		return "", fmt.Errorf("at least one column required")
	}
	idxName, err := quoteIdent(k, def.Name)
	if err != nil {
		return "", err
	}
	tbl, err := qualified(k, def.Schema, def.Table)
	if err != nil {
		return "", err
	}
	cols := make([]string, len(def.Columns))
	for i, c := range def.Columns {
		q, err := quoteIdent(k, c)
		if err != nil {
			return "", err
		}
		cols[i] = q
	}
	stmt := "CREATE "
	if def.Unique {
		stmt += "UNIQUE "
	}
	stmt += "INDEX " + idxName + " ON " + tbl
	if k == KindPostgres && def.Method != "" {
		stmt += " USING " + def.Method
	}
	stmt += " (" + strings.Join(cols, ", ") + ")"
	return stmt, nil
}

// synthesisedCreateTable renders a best-effort CREATE TABLE for display. Not for execution.
func synthesisedCreateTable(k Kind, ref ObjectRef, cols []Column, idx []IndexInfo, fks []ForeignKey) string {
	var b strings.Builder
	tbl, err := qualified(k, ref.Schema, ref.Name)
	if err != nil {
		return "-- " + err.Error()
	}
	b.WriteString("CREATE TABLE " + tbl + " (\n")
	pks := []string{}
	for i, c := range cols {
		colQ, _ := quoteIdent(k, c.Name)
		line := "  " + colQ + " " + c.DataType
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.Default != "" {
			line += " DEFAULT " + c.Default
		}
		if c.IsPK {
			pks = append(pks, colQ)
		}
		if i < len(cols)-1 || len(pks) > 0 || len(fks) > 0 {
			line += ","
		}
		b.WriteString(line + "\n")
	}
	if len(pks) > 0 {
		suffix := ""
		if len(fks) > 0 {
			suffix = ","
		}
		b.WriteString("  PRIMARY KEY (" + strings.Join(pks, ", ") + ")" + suffix + "\n")
	}
	for i, fk := range fks {
		refTbl, _ := qualified(k, fk.RefSchema, fk.RefTable)
		colsQ := make([]string, len(fk.Columns))
		for j, c := range fk.Columns {
			colsQ[j], _ = quoteIdent(k, c)
		}
		refColsQ := make([]string, len(fk.RefColumns))
		for j, c := range fk.RefColumns {
			refColsQ[j], _ = quoteIdent(k, c)
		}
		line := "  FOREIGN KEY (" + strings.Join(colsQ, ", ") + ") REFERENCES " + refTbl + " (" + strings.Join(refColsQ, ", ") + ")"
		if i < len(fks)-1 {
			line += ","
		}
		b.WriteString(line + "\n")
	}
	b.WriteString(");\n")
	for _, ix := range idx {
		if ix.Primary {
			continue
		}
		idxName, _ := quoteIdent(k, ix.Name)
		colsQ := make([]string, len(ix.Columns))
		for j, c := range ix.Columns {
			colsQ[j], _ = quoteIdent(k, c)
		}
		uniq := ""
		if ix.Unique {
			uniq = "UNIQUE "
		}
		b.WriteString("CREATE " + uniq + "INDEX " + idxName + " ON " + tbl + " (" + strings.Join(colsQ, ", ") + ");\n")
	}
	return b.String()
}

package database

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// 4 queries in total (not one per table), so it stays fast on large schemas.
// Every query filters on DATABASE(): only the configured database is inspected.
const (
	tablesSQL = `
SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT, TABLE_ROWS
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE()
ORDER BY TABLE_NAME`

	columnsSQL = `
SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_COMMENT
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE()
ORDER BY TABLE_NAME, ORDINAL_POSITION`

	indexesSQL = `
SELECT TABLE_NAME, INDEX_NAME, NON_UNIQUE, COLUMN_NAME
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND COLUMN_NAME IS NOT NULL
ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`

	foreignKeysSQL = `
SELECT TABLE_NAME, CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = DATABASE()
  AND REFERENCED_TABLE_SCHEMA = DATABASE()
  AND REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION`
)

// Introspect reads the live structure of the configured database.
func Introspect(ctx context.Context, db *sql.DB) (*Schema, error) {
	schema := &Schema{CapturedAt: time.Now().UTC()}
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema.Database); err != nil {
		return nil, SafeError(err)
	}

	byName := map[string]*Table{}

	err := eachRow(ctx, db, tablesSQL, func(rows *sql.Rows) error {
		var name, kind string
		var comment sql.NullString
		var estimate sql.NullInt64
		if err := rows.Scan(&name, &kind, &comment, &estimate); err != nil {
			return err
		}
		t := &Table{Name: name, Kind: "table", Comment: comment.String}
		if kind == "VIEW" {
			t.Kind, t.Comment = "view", "" // MySQL sets a view's comment to "VIEW"
		} else if estimate.Valid {
			t.RowEstimate = &estimate.Int64
		}
		schema.Tables = append(schema.Tables, t)
		byName[name] = t
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, db, columnsSQL, func(rows *sql.Rows) error {
		var table, name, typ, nullable string
		var dflt, comment sql.NullString
		if err := rows.Scan(&table, &name, &typ, &nullable, &dflt, &comment); err != nil {
			return err
		}
		if t := byName[table]; t != nil {
			col := Column{Name: name, Type: typ, Nullable: nullable == "YES", Comment: comment.String}
			if dflt.Valid {
				col.Default = &dflt.String
			}
			t.Columns = append(t.Columns, col)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Rows arrive ordered by table, index, position: group consecutive rows.
	err = eachRow(ctx, db, indexesSQL, func(rows *sql.Rows) error {
		var table, name, column string
		var nonUnique int
		if err := rows.Scan(&table, &name, &nonUnique, &column); err != nil {
			return err
		}
		t := byName[table]
		if t == nil {
			return nil
		}
		if name == "PRIMARY" {
			t.PrimaryKey = append(t.PrimaryKey, column)
			return nil
		}
		if n := len(t.Indexes); n > 0 && t.Indexes[n-1].Name == name {
			t.Indexes[n-1].Columns = append(t.Indexes[n-1].Columns, column)
		} else {
			t.Indexes = append(t.Indexes, Index{Name: name, Columns: []string{column}, Unique: nonUnique == 0})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, db, foreignKeysSQL, func(rows *sql.Rows) error {
		var table, name, column, refTable, refColumn string
		if err := rows.Scan(&table, &name, &column, &refTable, &refColumn); err != nil {
			return err
		}
		t := byName[table]
		if t == nil {
			return nil
		}
		if n := len(t.ForeignKeys); n > 0 && t.ForeignKeys[n-1].Name == name {
			fk := &t.ForeignKeys[n-1]
			fk.Columns = append(fk.Columns, column)
			fk.RefColumns = append(fk.RefColumns, refColumn)
		} else {
			t.ForeignKeys = append(t.ForeignKeys, ForeignKey{
				Name: name, Columns: []string{column}, RefTable: refTable, RefColumns: []string{refColumn},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	InferRelationships(schema.Tables)
	schema.normalize()
	return schema, nil
}

func eachRow(ctx context.Context, db *sql.DB, query string, fn func(*sql.Rows) error) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return SafeError(err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return SafeError(err)
		}
	}
	return SafeError(rows.Err())
}

func candidateNames(base string) []string {
	names := []string{base, base + "s", base + "es"}
	if strings.HasSuffix(base, "y") {
		names = append(names, base[:len(base)-1]+"ies")
	}
	return names
}

// InferRelationships adds guessed links for `<thing>_id` columns without a real foreign key.
// Example: order_items.order_id -> orders.id. Marked Inferred so the AI treats them with care.
func InferRelationships(tables []*Table) {
	byName := map[string]*Table{}
	for _, t := range tables {
		byName[strings.ToLower(t.Name)] = t
	}
	for _, t := range tables {
		if t.Kind == "view" {
			continue
		}
		linked := map[string]bool{}
		for _, fk := range t.ForeignKeys {
			for _, c := range fk.Columns {
				linked[strings.ToLower(c)] = true
			}
		}
		for _, col := range t.Columns {
			lower := strings.ToLower(col.Name)
			if !strings.HasSuffix(lower, "_id") || len(lower) <= 3 || linked[lower] {
				continue
			}
			var target *Table
			for _, candidate := range candidateNames(strings.TrimSuffix(lower, "_id")) {
				if found, ok := byName[candidate]; ok {
					target = found
					break
				}
			}
			if target == nil || target == t || len(target.PrimaryKey) != 1 {
				continue
			}
			t.ForeignKeys = append(t.ForeignKeys, ForeignKey{
				Columns:    []string{col.Name},
				RefTable:   target.Name,
				RefColumns: []string{target.PrimaryKey[0]},
				Inferred:   true,
			})
		}
	}
}

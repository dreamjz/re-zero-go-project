package clause

import (
	"fmt"
	"strings"
)

type generator func(vals ...any) (string, []any)

var generators map[Type]generator

func init() {
	generators = make(map[Type]generator)
	generators[INSERT] = _insert
	generators[VALUES] = _values
	generators[SELECT] = _select
	generators[LIMIT] = _limit
	generators[WHERE] = _where
	generators[ORDERBY] = _orderBy
	generators[UPDATE] = _update
	generators[DELETE] = _delete
	generators[COUNT] = _count
}

func genBindVars(num int) string {
	var vars []string
	for i := 0; i < num; i++ {
		vars = append(vars, "?")
	}
	return strings.Join(vars, ", ")
}

func _insert(vals ...any) (string, []any) {
	// INSERT INTO $tableName ($fields)
	tableName := vals[0]
	fields := strings.Join(vals[1].([]string), ",")
	return fmt.Sprintf("INSERT INTO %s (%v)", tableName, fields), []any{}
}

func _values(vals ...any) (string, []any) {
	// VALUES ($1), ($2) ...
	var (
		bindStr string
		sql     strings.Builder
		vars    []any
	)

	sql.WriteString("VALUES ")
	for i, val := range vals {
		v := val.([]any)
		if bindStr == "" {
			bindStr = genBindVars(len(v))
		}
		sql.WriteString(fmt.Sprintf("(%v)", bindStr))
		if i+1 != len(vals) {
			sql.WriteString(", ")
		}
		vars = append(vars, v...)
	}
	return sql.String(), vars
}

func _select(vals ...any) (string, []any) {
	// SELECT $fields FROM $tableName
	tableName := vals[0]
	fields := strings.Join(vals[1].([]string), ",")
	return fmt.Sprintf("SELECT %v FROM %s", fields, tableName), []any{}
}

func _limit(vals ...any) (string, []any) {
	// LIMIT $num
	return "LIMIT ?", vals
}

func _where(vals ...any) (string, []any) {
	// WHERE $desc
	desc, vars := vals[0], vals[1:]
	return fmt.Sprintf("WHERE %s", desc), vars
}

func _orderBy(vals ...any) (string, []any) {
	return fmt.Sprintf("ORDER BY %s", vals[0]), []any{}
}

func _update(vals ...any) (string, []any) {
	tableName := vals[0]
	m := vals[1].(map[string]any)
	var (
		keys []string
		vars []any
	)
	for k, v := range m {
		keys = append(keys, k+" = ?")
		vars = append(vars, v)
	}
	return fmt.Sprintf("UPDATE %s SET %s", tableName, strings.Join(keys, ", ")), vars
}

func _delete(vals ...any) (string, []any) {
	return fmt.Sprintf("DELETE FROM %s", vals[0]), []any{}
}

func _count(vals ...any) (string, []any) {
	return _select(vals[0], []string{"count(*)"})
}

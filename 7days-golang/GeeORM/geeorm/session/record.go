package session

import (
	"errors"
	"geeorm/clause"
	"reflect"
)

func (s *Session) Insert(vals ...any) (int64, error) {
	recordVals := make([]any, 0)
	for _, val := range vals {
		s.CallMethod(BeforeInsert, val)
		table := s.Model(val).RefTable()
		s.clause.Set(clause.INSERT, table.Name, table.FieldNames)
		recordVals = append(recordVals, table.RecordValue(val))
	}

	s.clause.Set(clause.VALUES, recordVals...)
	sql, vars := s.clause.Build(clause.INSERT, clause.VALUES)
	res, err := s.Raw(sql, vars...).Exec()
	if err != nil {
		return 0, err
	}
	s.CallMethod(AfterInsert, nil)
	return res.RowsAffected()
}

func (s *Session) Find(vals any) error {
	s.CallMethod(BeforeQuery, nil)
	dstSlice := reflect.Indirect(reflect.ValueOf(vals))
	dstType := dstSlice.Type().Elem()
	table := s.Model(reflect.New(dstType).Elem().Interface()).RefTable()

	s.clause.Set(clause.SELECT, table.Name, table.FieldNames)
	sql, vars := s.clause.Build(clause.SELECT, clause.WHERE, clause.ORDERBY, clause.LIMIT)
	rows, err := s.Raw(sql, vars...).QueryRows()
	if err != nil {
		return err
	}

	for rows.Next() {
		dst := reflect.New(dstType).Elem()
		var fieldVals []any
		for _, name := range table.FieldNames {
			fieldVals = append(fieldVals, dst.FieldByName(name).Addr().Interface())
		}
		if err := rows.Scan(fieldVals...); err != nil {
			return err
		}
		s.CallMethod(AfterQuery, dst.Addr().Interface())
		dstSlice.Set(reflect.Append(dstSlice, dst))
	}
	return rows.Close()
}

// Update updates records of table
// support map[string]any
// kv list: string, any, string, any, ...
func (s *Session) Update(kvs ...any) (int64, error) {
	s.CallMethod(BeforeUpdate, nil)
	m, ok := kvs[0].(map[string]any)
	if !ok {
		m = make(map[string]any)
		for i := 0; i < len(kvs); i += 2 {
			m[kvs[i].(string)] = kvs[i+1]
		}
	}
	s.clause.Set(clause.UPDATE, s.RefTable().Name, m)
	sql, vars := s.clause.Build(clause.UPDATE, clause.WHERE)
	res, err := s.Raw(sql, vars...).Exec()
	if err != nil {
		return 0, err
	}
	s.CallMethod(AfterUpdate, nil)
	return res.RowsAffected()
}

func (s *Session) Delete() (int64, error) {
	s.CallMethod(BeforeDelete, nil)
	s.clause.Set(clause.DELETE, s.RefTable().Name)
	sql, vars := s.clause.Build(clause.DELETE, clause.WHERE)
	res, err := s.Raw(sql, vars...).Exec()
	if err != nil {
		return 0, err
	}
	s.CallMethod(AfterDelete, nil)
	return res.RowsAffected()
}

func (s *Session) Count() (int64, error) {
	s.clause.Set(clause.COUNT, s.RefTable().Name)
	sql, vars := s.clause.Build(clause.COUNT, clause.WHERE)
	row := s.Raw(sql, vars...).QueryRow()
	var tmp int64
	if err := row.Scan(&tmp); err != nil {
		return 0, err
	}
	return tmp, nil
}

func (s *Session) Limit(num int) *Session {
	s.clause.Set(clause.LIMIT, num)
	return s
}

func (s *Session) Where(desc string, args ...any) *Session {
	var vars []any
	s.clause.Set(clause.WHERE, append(append(vars, desc), args...)...)
	return s
}

func (s *Session) OrderBy(desc string) *Session {
	s.clause.Set(clause.ORDERBY, desc)
	return s
}

func (s *Session) First(val any) error {
	dst := reflect.Indirect(reflect.ValueOf(val))
	dstSlice := reflect.New(reflect.SliceOf(dst.Type())).Elem()
	if err := s.Limit(1).Find(dstSlice.Addr().Interface()); err != nil {
		return err
	}
	if dstSlice.Len() == 0 {
		return errors.New("NOT FOUND")
	}
	dst.Set(dstSlice.Index(0))
	return nil
}

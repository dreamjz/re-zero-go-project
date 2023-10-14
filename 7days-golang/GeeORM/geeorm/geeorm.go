package geeorm

import (
	"database/sql"
	"errors"
	"fmt"
	"geeorm/dialect"
	"geeorm/log"
	"geeorm/session"
	"strings"
)

var (
	DialectNotFoundErr = errors.New("dialect not found")
)

type Engine struct {
	db      *sql.DB
	dialect dialect.Dialect
}

func NewEngine(driver, source string) (*Engine, error) {
	db, err := sql.Open(driver, source)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// Send ping to make sure the database connection is alive
	if err = db.Ping(); err != nil {
		log.Error(err)
		return nil, err
	}

	// make sure the specific dialect exists
	d, ok := dialect.GetDialect(driver)
	if !ok {
		log.Errorf("dialect %s Not Found", driver)
		return nil, DialectNotFoundErr
	}

	e := &Engine{db: db, dialect: d}
	log.Info("Connect database success")

	return e, nil
}

func (engine *Engine) Close() {
	if err := engine.db.Close(); err != nil {
		log.Error("Failed to close database connection")
		return
	}
	log.Info("Close database connection success")
}

func (engine *Engine) NewSession() *session.Session {
	return session.New(engine.db, engine.dialect)
}

type TxFunc func(s *session.Session) (any, error)

func (engine *Engine) Transaction(f TxFunc) (res any, err error) {
	s := engine.NewSession()
	if err = s.Begin(); err != nil {
		return nil, err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = s.Rollback()
			panic(p) // re-throw panic after rollback
		} else if err != nil {
			_ = s.Rollback() // err is non-nil
		} else {
			err = s.Commit() // err is nil; if Commit returns error, update err
		}
	}()

	return f(s)
}

// return a - b
func difference(a []string, b []string) []string {
	setB := make(map[string]struct{})
	for _, v := range b {
		setB[v] = struct{}{}
	}

	diff := make([]string, 0)
	for _, v := range a {
		if _, ok := setB[v]; !ok {
			diff = append(diff, v)
		}
	}
	return diff
}

// Migrate table
func (engine *Engine) Migrate(value any) error {
	_, err := engine.Transaction(func(s *session.Session) (any, error) {
		if !s.Model(value).HasTable() {
			log.Infof("table %s doesn't exist, creat table", s.RefTable().Name)
			return nil, s.CreateTable()
		}
		table := s.RefTable()
		rows, _ := s.Raw(fmt.Sprintf("SELECT * FROM %s LIMIT 1", table.Name)).QueryRows()
		columns, _ := rows.Columns()
		addCols := difference(table.FieldNames, columns)
		delCols := difference(columns, table.FieldNames)
		log.Info("add cols %v, deleted cols %v", addCols, delCols)

		for _, col := range addCols {
			f := table.GetField(col)
			sqlStr := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table.Name, f.Name, f.Type)
			if _, err := s.Raw(sqlStr).Exec(); err != nil {
				return nil, err
			}
		}

		if len(delCols) == 0 {
			return nil, nil
		}
		tmp := "tmp_" + table.Name
		fieldStr := strings.Join(table.FieldNames, ", ")
		s.Raw(fmt.Sprintf("CREATE TABLE %s AS SELECT %s FROM %s;", tmp, fieldStr, table.Name))
		s.Raw(fmt.Sprintf("DROP TABLE %s;", table.Name))
		s.Raw(fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", tmp, table.Name))
		_, err := s.Exec()
		return nil, err
	})

	return err
}

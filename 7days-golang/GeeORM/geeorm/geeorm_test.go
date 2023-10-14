package geeorm

import (
	"errors"
	"geeorm/session"
	_ "github.com/mattn/go-sqlite3"
	"reflect"
	"testing"
)

func openDB(t *testing.T) *Engine {
	t.Helper()
	engine, err := NewEngine("sqlite3", "gee.db")
	if err != nil {
		t.Fatal("failed to connect database")
	}
	return engine
}

func TestNewEngine(t *testing.T) {
	engine := openDB(t)
	defer engine.Close()
}

type User struct {
	Name string `geeorm:"RPIMARY KEY"`
	Age  int
}

func TestEngine_Transaction(t *testing.T) {
	tests := []struct {
		name string
		f    func(t *testing.T)
	}{
		{name: "Rollback", f: testRollback},
		{name: "Commit", f: testCommit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f(t)
		})
	}
}

func testRollback(t *testing.T) {
	engine := openDB(t)
	defer engine.Close()
	s := engine.NewSession()
	_ = s.Model(&User{}).DropTable()
	_, err := engine.Transaction(func(s *session.Session) (any, error) {
		_ = s.Model(&User{}).CreateTable()
		_, _ = s.Insert(&User{"Tom", 18})
		return nil, errors.New("inert error")
	})

	if err == nil || s.HasTable() {
		t.Fatal("failed to rollback")
	}
}

func testCommit(t *testing.T) {
	engine := openDB(t)
	defer engine.Close()
	s := engine.NewSession()
	_ = s.Model(&User{}).DropTable()
	_, err := engine.Transaction(func(s *session.Session) (any, error) {
		_ = s.Model(&User{}).CreateTable()
		_, err := s.Insert(&User{"Tom", 18})
		return nil, err
	})

	u := &User{}
	_ = s.First(u)

	if err != nil || u.Name != "Tom" {
		t.Fatal("failed to commit")
	}
}

func TestEngine_Migrate(t *testing.T) {
	engine := openDB(t)
	defer engine.Close()
	s := engine.NewSession()

	// Migrate User{Name string, XXX int} to User{Name string, Age int}
	_, _ = s.Raw("DROP TABLE IF EXISTS User;").Exec()
	_, _ = s.Raw("CREATE TABLE User(Name text PRIMARY KEY, XXX integer);").Exec()
	_, _ = s.Raw("INSERT INTO User(`Name`) values (?), (?)", "Tom", "Sam").Exec()
	engine.Migrate(&User{})

	rows, _ := s.Raw("SELECT * FROM User").QueryRows()
	cols, _ := rows.Columns()
	if !reflect.DeepEqual(cols, []string{"Name", "Age"}) {
		t.Fatal("failed to migrate table User, got cols:", cols)
	}

}

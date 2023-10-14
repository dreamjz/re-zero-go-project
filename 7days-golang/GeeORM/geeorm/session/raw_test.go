package session

import (
	"database/sql"
	"geeorm/dialect"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"testing"
)

var (
	testDB         *sql.DB
	testDialect, _ = dialect.GetDialect("sqlite3")
)

func TestMain(m *testing.M) {
	testDB, _ = sql.Open("sqlite3", "../gee.db")
	code := m.Run()
	_ = testDB.Close()
	os.Exit(code)
}

func newSession() *Session {
	return New(testDB, testDialect)
}

func TestSessionExec(t *testing.T) {
	s := newSession()
	_, _ = s.Raw("DROP TABLE IF EXISTS User;").Exec()
	_, _ = s.Raw("CREATE TABLE User(Name text);").Exec()
	res, _ := s.Raw("INSERT INTO User(`Name`) VALUES (?), (?)", "Alice", "Ai").Exec()
	if c, err := res.RowsAffected(); err != nil || c != 2 {
		t.Fatal("Expected 2, but got", c)
	}
}

func TestSession_QueryRows(t *testing.T) {
	s := newSession()
	_, _ = s.Raw("DROP TABLE IF EXISTS User;").Exec()
	_, _ = s.Raw("CREATE TABLE User(Name text);").Exec()
	row := s.Raw("SELECT count(*) FROM User").QueryRow()
	var count int
	if err := row.Scan(&count); err != nil || count != 0 {
		t.Fatal("failed to query db", err)
	}
}

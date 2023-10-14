package session

import (
	"geeorm/log"
	"testing"
)

type Account struct {
	ID       int `geeorm:"PRIMARY KEY"`
	Password string
}

const (
	pass = "******"
)

func (a *Account) BeforeInsert(s *Session) error {
	log.Info("before insert", a)
	a.ID += 1000
	return nil
}

func (a *Account) AfterQuery(s *Session) error {
	log.Info("after query", a)
	a.Password = pass
	return nil
}

func TestSession_CallMethod(t *testing.T) {
	s := newSession().Model(&Account{})
	_ = s.DropTable()
	_ = s.CreateTable()
	_, _ = s.Insert(&Account{1, "123"}, &Account{2, "abc"})

	u := &Account{}

	err := s.First(u)
	if err != nil || u.ID != 1001 || u.Password != pass {
		t.Fatal("failed to call after query hooks")
	}

}

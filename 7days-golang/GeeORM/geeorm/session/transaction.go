package session

import "geeorm/log"

func (s *Session) Begin() error {
	log.Info("transaction begin")
	var err error
	if s.tx, err = s.db.Begin(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (s *Session) Commit() error {
	log.Info("transaction commit")
	if err := s.tx.Commit(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (s *Session) Rollback() error {
	log.Info("transaction rollback")
	if err := s.tx.Rollback(); err != nil {
		log.Error(err)
		return err
	}
	return nil
}

package database

import (
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/lib/pq"
)

var (
	instance *sql.DB
	once     sync.Once
)

func GetConnection(dsn string) (*sql.DB, error) {
	var err error
	once.Do(func() {
		instance, err = sql.Open("postgres", dsn)
		if err != nil {
			return
		}
		err = instance.Ping()
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return instance, nil
}


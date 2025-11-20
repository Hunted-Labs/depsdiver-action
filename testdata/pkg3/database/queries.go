package database

import (
	"context"
	"database/sql"
	"time"
)

type User struct {
	ID        int64
	Name      string
	Email     string
	CreatedAt time.Time
}

func GetUserByID(ctx context.Context, db *sql.DB, id int64) (*User, error) {
	query := "SELECT id, name, email, created_at FROM users WHERE id = $1"
	row := db.QueryRowContext(ctx, query, id)

	var user User
	err := row.Scan(&user.ID, &user.Name, &user.Email, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func CreateUser(ctx context.Context, db *sql.DB, name, email string) (*User, error) {
	query := "INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id, created_at"
	row := db.QueryRowContext(ctx, query, name, email)

	var user User
	user.Name = name
	user.Email = email
	err := row.Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &user, nil
}


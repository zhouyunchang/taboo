package store

import "errors"

func IsUniqueViolation(err error) bool {
	var pg interface{ SQLState() string }
	if errors.As(err, &pg) {
		return pg.SQLState() == "23505"
	}
	var sqlite interface{ Code() int }
	return errors.As(err, &sqlite) && (sqlite.Code() == 1555 || sqlite.Code() == 2067)
}

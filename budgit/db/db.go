package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type DB struct {
	log *zap.SugaredLogger
}

func New(log *zap.SugaredLogger) DB {
	return DB{log: log}
}

func (db DB) Now(ctx context.Context, queryer Queryer) (pgtype.Timestamptz, error) {
	db.log.Debug("Selecting Now timestamp")

	row := queryer.QueryRow(ctx, `SELECT NOW()`)
	var now pgtype.Timestamptz
	if err := row.Scan(&now); err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("selecting Now: %w", err)
	}
	db.log.Debug("Selected Now")

	return now, nil
}

type ValidToTimestampUpdate struct {
	ID               pgtype.Text        `db:"id"`
	ValidToTimestamp pgtype.Timestamptz `db:"valid_to_timestamp"`
}

func rowsToIDs(rows pgx.Rows) ([]string, error) {
	ids := make([]string, 0, rows.CommandTag().RowsAffected())
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning rows: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func structsToPointers[E any](elems []E) []*E {
	ptrElems := make([]*E, 0, len(elems))
	for _, elem := range elems {
		ptrElems = append(ptrElems, &elem)
	}
	return ptrElems
}

const dbStructKey = "db"

func getAllDBColumns(strct any) []string {
	elem := reflect.TypeOf(strct)
	columns := make([]string, 0, elem.NumField())
	for i := range elem.NumField() {
		columns = append(columns, string(elem.FieldByIndex([]int{i}).Tag.Get(dbStructKey)))
	}
	return columns
}

type requestIDGetter interface {
	GetRequestID() string
}

func mapByRequestID[E requestIDGetter](elems []E) map[string]E {
	elemsByRequestID := make(map[string]E, len(elems))
	for _, elem := range elems {
		elemsByRequestID[elem.GetRequestID()] = elem
	}
	return elemsByRequestID
}

type idGetter interface {
	GetID() string
}

func mapByID[E idGetter](elems []E) map[string]E {
	elemsByID := make(map[string]E, len(elems))
	for _, elem := range elems {
		elemsByID[elem.GetID()] = elem
	}
	return elemsByID
}

type nameGetter interface {
	GetName() string
}

func mapByName[E nameGetter](elems []E) map[string]E {
	elemsByName := make(map[string]E, len(elems))
	for _, elem := range elems {
		elemsByName[elem.GetName()] = elem
	}
	return elemsByName
}

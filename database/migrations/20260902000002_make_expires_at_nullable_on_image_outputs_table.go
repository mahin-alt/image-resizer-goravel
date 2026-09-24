package migrations

import (
	"fmt"

	"goravel/app/facades"
)

// Generated outputs are permanent now (see app/jobs/process_image_request_job.go
// and the removed cleanup schedule in bootstrap/schedule.go) - new rows no
// longer set expires_at, so the column must allow NULL. There's no
// Blueprint.Change() in this framework version to alter an existing
// column's nullability, hence the raw SQL. The two supported connections
// (see config/database.go) use different ALTER COLUMN syntax, so the
// statement is picked based on the active "database.default" connection.
type M20260902000002MakeExpiresAtNullableOnImageOutputsTable struct{}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Signature() string {
	return "20260902000002_make_expires_at_nullable_on_image_outputs_table"
}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Up() error {
	sql, err := r.sqlFor("ALTER TABLE image_outputs MODIFY expires_at DATETIME NULL",
		"ALTER TABLE image_outputs ALTER COLUMN expires_at DROP NOT NULL")
	if err != nil {
		return err
	}
	_, err = facades.Orm().Query().Exec(sql)
	return err
}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Down() error {
	sql, err := r.sqlFor("ALTER TABLE image_outputs MODIFY expires_at DATETIME NOT NULL",
		"ALTER TABLE image_outputs ALTER COLUMN expires_at SET NOT NULL")
	if err != nil {
		return err
	}
	_, err = facades.Orm().Query().Exec(sql)
	return err
}

// sqlFor returns the mysql or postgres statement matching the active
// "database.default" connection, since goravel/mysql and goravel/postgres
// are the only two connections defined in config/database.go.
func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) sqlFor(mysqlSQL, postgresSQL string) (string, error) {
	switch connection := facades.Config().GetString("database.default"); connection {
	case "mysql":
		return mysqlSQL, nil
	case "postgres":
		return postgresSQL, nil
	default:
		return "", fmt.Errorf("unsupported database connection %q for raw ALTER TABLE migration", connection)
	}
}

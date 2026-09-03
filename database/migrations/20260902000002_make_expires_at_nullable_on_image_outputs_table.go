package migrations

import (
	"goravel/app/facades"
)

// Generated outputs are permanent now (see app/jobs/process_image_request_job.go
// and the removed cleanup schedule in bootstrap/schedule.go) - new rows no
// longer set expires_at, so the column must allow NULL. There's no
// Blueprint.Change() in this framework version to alter an existing
// column's nullability, hence the raw SQL.
type M20260902000002MakeExpiresAtNullableOnImageOutputsTable struct{}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Signature() string {
	return "20260902000002_make_expires_at_nullable_on_image_outputs_table"
}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Up() error {
	_, err := facades.Orm().Query().Exec("ALTER TABLE image_outputs MODIFY expires_at DATETIME NULL")
	return err
}

func (r *M20260902000002MakeExpiresAtNullableOnImageOutputsTable) Down() error {
	_, err := facades.Orm().Query().Exec("ALTER TABLE image_outputs MODIFY expires_at DATETIME NOT NULL")
	return err
}

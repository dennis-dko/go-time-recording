// Package migration defines the database schema as GoFr migrations, so a
// fresh binary provisions its own database on first start and no external
// schema tooling is needed for deployment.
//
// GoFr's own package has the same name and is imported here as it is, so inside
// this package migration.Migrate and migration.Datasource are GoFr's. A file
// outside that imports both names GoFr's gofrmigration.
package migration

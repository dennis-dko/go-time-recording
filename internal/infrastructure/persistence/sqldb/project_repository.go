package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
	"github.com/dennis-dko/go-time-recording/internal/domain/repository"
	"github.com/dennis-dko/go-time-recording/internal/pkg/apperror"
)

const projectColumns = "id, name, description, start_date, end_date, status, owner_id"

// ProjectRepository stores projects in a SQL database.
type ProjectRepository struct {
	base
}

// NewProjectRepository creates a project repository for the given dialect.
func NewProjectRepository(db DB, dialect string) *ProjectRepository {
	return &ProjectRepository{base{db: db, dialect: dialect}}
}

var _ repository.ProjectRepository = (*ProjectRepository)(nil)

func (r *ProjectRepository) Save(ctx context.Context, project *model.Project) (*model.Project, error) {
	id, err := r.insert(ctx,
		"INSERT INTO projects (name, description, start_date, end_date, status, owner_id) "+
			"VALUES (?, ?, ?, ?, ?, ?)",
		project.Name, project.Description, project.StartDate, project.EndDate,
		project.Status, project.OwnerID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	created := *project
	created.ID = id

	return &created, nil
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uint) (*model.Project, error) {
	row := r.db.QueryRowContext(ctx, r.rebind("SELECT "+projectColumns+" FROM projects WHERE id = ?"), id)

	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperror.NotFound("project", strconv.FormatUint(uint64(id), 10))
	}

	if err != nil {
		return nil, apperror.Internal(err)
	}

	return project, nil
}

func (r *ProjectRepository) GetAll(ctx context.Context) ([]*model.Project, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+projectColumns+" FROM projects ORDER BY id")
	if err != nil {
		return nil, apperror.Internal(err)
	}
	defer func() { _ = rows.Close() }()

	projects := make([]*model.Project, 0)

	for rows.Next() {
		project, scanErr := scanProject(rows)
		if scanErr != nil {
			return nil, apperror.Internal(scanErr)
		}

		projects = append(projects, project)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.Internal(err)
	}

	return projects, nil
}

func (r *ProjectRepository) Update(ctx context.Context, project *model.Project) (*model.Project, error) {
	affected, err := r.exec(ctx,
		"UPDATE projects SET name = ?, description = ?, start_date = ?, end_date = ?, "+
			"status = ?, owner_id = ? WHERE id = ?",
		project.Name, project.Description, project.StartDate, project.EndDate,
		project.Status, project.OwnerID, project.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if affected == 0 {
		return nil, apperror.NotFound("project", strconv.FormatUint(uint64(project.ID), 10))
	}

	updated := *project

	return &updated, nil
}

func (r *ProjectRepository) Delete(ctx context.Context, id uint) error {
	affected, err := r.exec(ctx, "DELETE FROM projects WHERE id = ?", id)
	if err != nil {
		return apperror.Internal(err)
	}

	if affected == 0 {
		return apperror.NotFound("project", strconv.FormatUint(uint64(id), 10))
	}

	return nil
}

func scanProject(s scanner) (*model.Project, error) {
	var (
		project   model.Project
		startDate dateTime
		endDate   dateTime
	)

	err := s.Scan(&project.ID, &project.Name, &project.Description, &startDate, &endDate,
		&project.Status, &project.OwnerID)
	if err != nil {
		return nil, err
	}

	project.StartDate = startDate.Time
	project.EndDate = ptr(endDate)

	return &project, nil
}

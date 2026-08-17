package store

import (
	"context"
	"database/sql"
)

type ScanProject struct {
	ID        int64
	UserID    int64
	Name      string
	PageCount int
	CreatedAt string
	UpdatedAt string
}

type ScanPage struct {
	ID           int64
	ProjectID    int64
	Position     int
	OriginalPath string
	EditedPath   string
	CreatedAt    string
}

func CreateScanProject(ctx context.Context, tx *sql.Tx, userID int64, name string) (ScanProject, error) {
	now := nowUTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO scan_projects (user_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?)`, userID, name, now, now)
	if err != nil {
		return ScanProject{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ScanProject{}, err
	}
	return GetScanProject(ctx, tx, userID, id)
}

func ListScanProjects(ctx context.Context, tx *sql.Tx, userID int64) ([]ScanProject, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id, p.user_id, p.name, COUNT(s.id), p.created_at, p.updated_at
		FROM scan_projects p
		LEFT JOIN scan_pages s ON s.project_id = p.id
		WHERE p.user_id = ?
		GROUP BY p.id
		ORDER BY p.updated_at DESC, p.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []ScanProject{}
	for rows.Next() {
		project, err := scanScanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func GetScanProject(ctx context.Context, tx *sql.Tx, userID, projectID int64) (ScanProject, error) {
	row := tx.QueryRowContext(ctx, `SELECT p.id, p.user_id, p.name, COUNT(s.id), p.created_at, p.updated_at
		FROM scan_projects p
		LEFT JOIN scan_pages s ON s.project_id = p.id
		WHERE p.id = ? AND p.user_id = ?
		GROUP BY p.id`, projectID, userID)
	return scanScanProject(row)
}

func CreateScanPage(ctx context.Context, tx *sql.Tx, userID, projectID int64, originalPath string) (ScanPage, error) {
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(s.position), 0) + 1
		FROM scan_pages s
		JOIN scan_projects p ON p.id = s.project_id
		WHERE s.project_id = ? AND p.user_id = ?`, projectID, userID).Scan(&position); err != nil {
		return ScanPage{}, err
	}
	if _, err := GetScanProject(ctx, tx, userID, projectID); err != nil {
		return ScanPage{}, err
	}
	now := nowUTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO scan_pages (project_id, position, original_path, created_at)
		VALUES (?, ?, ?, ?)`, projectID, position, originalPath, now)
	if err != nil {
		return ScanPage{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ScanPage{}, err
	}
	if err := touchScanProject(ctx, tx, projectID); err != nil {
		return ScanPage{}, err
	}
	return GetScanPage(ctx, tx, userID, projectID, id)
}

func GetScanPage(ctx context.Context, tx *sql.Tx, userID, projectID, pageID int64) (ScanPage, error) {
	row := tx.QueryRowContext(ctx, `SELECT s.id, s.project_id, s.position, s.original_path, s.edited_path, s.created_at
		FROM scan_pages s
		JOIN scan_projects p ON p.id = s.project_id
		WHERE s.id = ? AND s.project_id = ? AND p.user_id = ?`, pageID, projectID, userID)
	return scanScanPage(row)
}

func ListScanPages(ctx context.Context, tx *sql.Tx, userID, projectID int64) ([]ScanPage, error) {
	if _, err := GetScanProject(ctx, tx, userID, projectID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, project_id, position, original_path, edited_path, created_at
		FROM scan_pages WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := []ScanPage{}
	for rows.Next() {
		page, err := scanScanPage(rows)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, rows.Err()
}

func UpdateScanPage(ctx context.Context, tx *sql.Tx, userID, projectID, pageID int64, editedPath string) (string, error) {
	page, err := GetScanPage(ctx, tx, userID, projectID, pageID)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE scan_pages SET edited_path = ? WHERE id = ?", editedPath, page.ID); err != nil {
		return "", err
	}
	if err := touchScanProject(ctx, tx, projectID); err != nil {
		return "", err
	}
	return page.EditedPath, nil
}

func ResetScanPage(ctx context.Context, tx *sql.Tx, userID, projectID, pageID int64) (string, error) {
	page, err := GetScanPage(ctx, tx, userID, projectID, pageID)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE scan_pages SET edited_path = '' WHERE id = ?", page.ID); err != nil {
		return "", err
	}
	if err := touchScanProject(ctx, tx, projectID); err != nil {
		return "", err
	}
	return page.EditedPath, nil
}

func ReorderScanPages(ctx context.Context, tx *sql.Tx, userID, projectID int64, pageIDs []int64) error {
	pages, err := ListScanPages(ctx, tx, userID, projectID)
	if err != nil {
		return err
	}
	if len(pages) != len(pageIDs) {
		return sql.ErrNoRows
	}
	known := make(map[int64]struct{}, len(pages))
	for _, page := range pages {
		known[page.ID] = struct{}{}
	}
	for _, pageID := range pageIDs {
		if _, ok := known[pageID]; !ok {
			return sql.ErrNoRows
		}
		delete(known, pageID)
	}
	if len(known) != 0 {
		return sql.ErrNoRows
	}
	for index, pageID := range pageIDs {
		if _, err := tx.ExecContext(ctx, "UPDATE scan_pages SET position = ? WHERE id = ?", index+1, pageID); err != nil {
			return err
		}
	}
	return touchScanProject(ctx, tx, projectID)
}

func DeleteScanPage(ctx context.Context, tx *sql.Tx, userID, projectID, pageID int64) ([]string, error) {
	page, err := GetScanPage(ctx, tx, userID, projectID, pageID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM scan_pages WHERE id = ?", page.ID); err != nil {
		return nil, err
	}
	if err := touchScanProject(ctx, tx, projectID); err != nil {
		return nil, err
	}
	return scanPagePaths(page), nil
}

func DeleteScanProject(ctx context.Context, tx *sql.Tx, userID, projectID int64) ([]string, error) {
	pages, err := ListScanPages(ctx, tx, userID, projectID)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM scan_projects WHERE id = ? AND user_id = ?", projectID, userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		if err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	paths := make([]string, 0, len(pages)*2)
	for _, page := range pages {
		paths = append(paths, scanPagePaths(page)...)
	}
	return paths, nil
}

func touchScanProject(ctx context.Context, tx *sql.Tx, projectID int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE scan_projects SET updated_at = ? WHERE id = ?", nowUTC(), projectID)
	return err
}

func scanPagePaths(page ScanPage) []string {
	paths := []string{page.OriginalPath}
	if page.EditedPath != "" && page.EditedPath != page.OriginalPath {
		paths = append(paths, page.EditedPath)
	}
	return paths
}

func scanScanProject(s scanner) (ScanProject, error) {
	var project ScanProject
	err := s.Scan(&project.ID, &project.UserID, &project.Name, &project.PageCount, &project.CreatedAt, &project.UpdatedAt)
	return project, err
}

func scanScanPage(s scanner) (ScanPage, error) {
	var page ScanPage
	err := s.Scan(&page.ID, &page.ProjectID, &page.Position, &page.OriginalPath, &page.EditedPath, &page.CreatedAt)
	return page, err
}

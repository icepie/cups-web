package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanProjectPageLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "cups-web.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var ownerID, otherID, projectID, firstPageID, secondPageID int64
	if err := store.WithTx(ctx, false, func(tx *sql.Tx) error {
		owner, err := CreateUser(ctx, tx, CreateUserInput{Username: "owner", PasswordHash: "hash", Role: RoleUser})
		if err != nil {
			return err
		}
		other, err := CreateUser(ctx, tx, CreateUserInput{Username: "other", PasswordHash: "hash", Role: RoleUser})
		if err != nil {
			return err
		}
		project, err := CreateScanProject(ctx, tx, owner.ID, "报销单")
		if err != nil {
			return err
		}
		first, err := CreateScanPage(ctx, tx, owner.ID, project.ID, "20260817/original-1.png")
		if err != nil {
			return err
		}
		second, err := CreateScanPage(ctx, tx, owner.ID, project.ID, "20260817/original-2.png")
		if err != nil {
			return err
		}
		ownerID, otherID, projectID = owner.ID, other.ID, project.ID
		firstPageID, secondPageID = first.ID, second.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, true, func(tx *sql.Tx) error {
		projects, err := ListScanProjects(ctx, tx, ownerID)
		if err != nil {
			return err
		}
		if len(projects) != 1 || projects[0].Name != "报销单" || projects[0].PageCount != 2 {
			t.Fatalf("projects = %#v, want one project with two pages", projects)
		}
		_, err = GetScanProject(ctx, tx, otherID, projectID)
		if err != sql.ErrNoRows {
			t.Fatalf("other user project access error = %v, want sql.ErrNoRows", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, false, func(tx *sql.Tx) error {
		oldPath, err := UpdateScanPage(ctx, tx, ownerID, projectID, firstPageID, "20260817/edited-1.png")
		if err != nil {
			return err
		}
		if oldPath != "" {
			t.Fatalf("first edit old path = %q, want empty", oldPath)
		}
		return ReorderScanPages(ctx, tx, ownerID, projectID, []int64{secondPageID, firstPageID})
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, true, func(tx *sql.Tx) error {
		pages, err := ListScanPages(ctx, tx, ownerID, projectID)
		if err != nil {
			return err
		}
		if got := []int64{pages[0].ID, pages[1].ID}; !reflect.DeepEqual(got, []int64{secondPageID, firstPageID}) {
			t.Fatalf("page order = %v, want [%d %d]", got, secondPageID, firstPageID)
		}
		if pages[1].EditedPath != "20260817/edited-1.png" {
			t.Fatalf("edited path = %q", pages[1].EditedPath)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.WithTx(ctx, false, func(tx *sql.Tx) error {
		paths, err := DeleteScanProject(ctx, tx, ownerID, projectID)
		if err != nil {
			return err
		}
		want := []string{"20260817/original-2.png", "20260817/original-1.png", "20260817/edited-1.png"}
		if !reflect.DeepEqual(paths, want) {
			t.Fatalf("deleted paths = %v, want %v", paths, want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

package main

import (
	"context"
	"fmt"
)

// UserCmd groups user management subcommands.
type UserCmd struct {
	Promote UserPromoteCmd `cmd:"promote" help:"Grant admin privileges to a user."`
	Demote  UserDemoteCmd  `cmd:"demote" help:"Remove admin privileges from a user."`
	Delete  UserDeleteCmd  `cmd:"delete" help:"Soft-delete a user account."`
}

// UserPromoteCmd sets is_admin = true for the given user.
type UserPromoteCmd struct {
	Email string `arg:"" help:"Email address of the user to promote."`
}

func (c *UserPromoteCmd) Run() error {
	return setUserAdmin(c.Email, true)
}

// UserDemoteCmd sets is_admin = false for the given user.
type UserDemoteCmd struct {
	Email string `arg:"" help:"Email address of the user to demote."`
}

func (c *UserDemoteCmd) Run() error {
	return setUserAdmin(c.Email, false)
}

// UserDeleteCmd soft-deletes a user by setting deleted_at.
type UserDeleteCmd struct {
	Email string `arg:"" help:"Email address of the user to delete."`
	Force bool   `help:"Skip confirmation prompt." short:"f"`
}

func (c *UserDeleteCmd) Run() error {
	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	if !c.Force {
		if !confirmWipe(fmt.Sprintf("This will soft-delete the user %q.", c.Email)) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	res, err := db.ExecContext(context.Background(),
		"UPDATE users SET deleted_at = now() WHERE email = $1 AND deleted_at IS NULL", c.Email)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active user found with email %q", c.Email)
	}

	fmt.Printf("User %q has been deleted.\n", c.Email)
	return nil
}

func setUserAdmin(email string, admin bool) error {
	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	res, err := db.ExecContext(context.Background(),
		"UPDATE users SET is_admin = $1, updated_at = now() WHERE email = $2 AND deleted_at IS NULL",
		admin, email)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active user found with email %q", email)
	}

	action := "promoted to admin"
	if !admin {
		action = "demoted from admin"
	}
	fmt.Printf("User %q has been %s.\n", email, action)
	return nil
}

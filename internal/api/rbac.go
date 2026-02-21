package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const publicRegistrationRole = "worker"

func (s *Server) resolveRole(ctx context.Context, role string) (int64, string, error) {
	roleName := strings.ToLower(strings.TrimSpace(role))
	if roleName == "" {
		return 0, "", errors.New("missing role")
	}

	var roleID int64
	var canonical string
	err := s.db.QueryRow(ctx, `SELECT id, name FROM roles WHERE name = $1`, roleName).Scan(&roleID, &canonical)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", errors.New("invalid role")
		}
		return 0, "", err
	}
	return roleID, canonical, nil
}

func (s *Server) loadAuthContext(ctx context.Context, userID int64) (string, map[string]struct{}, error) {
	if userID <= 0 {
		return "", nil, errors.New("invalid user")
	}

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var roleID int64
	var role, status string
	err := s.db.QueryRow(ctx, `
		SELECT u.role_id, r.name, u.status
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1 AND u.email_verified = true
	`, userID).Scan(&roleID, &role, &status)
	if err != nil {
		return "", nil, err
	}
	if strings.ToLower(strings.TrimSpace(status)) != "active" {
		return "", nil, errors.New("inactive user")
	}

	rows, err := s.db.Query(ctx, `
		SELECT p.key
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = $1
	`, roleID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()

	perms := make(map[string]struct{})
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return "", nil, err
		}
		perms[strings.TrimSpace(key)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}

	return strings.ToLower(strings.TrimSpace(role)), perms, nil
}

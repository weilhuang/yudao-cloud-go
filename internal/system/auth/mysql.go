package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// MySQL 实现用户、令牌和权限的查询。逻辑删除用 deleted=0。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) FindByUsername(ctx context.Context, tenantID int64, username string) (*User, error) {
	query := `SELECT id, tenant_id, username, password, nickname, IFNULL(avatar,''), IFNULL(email,''), dept_id, status
		FROM system_users WHERE username = ? AND deleted = 0`
	args := []any{username}
	if tenantID > 0 {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` ORDER BY id LIMIT 1`
	return m.scanUser(m.DB.QueryRowContext(ctx, query, args...))
}

func (m *MySQL) FindByID(ctx context.Context, id int64) (*User, error) {
	return m.scanUser(m.DB.QueryRowContext(ctx, `SELECT id, tenant_id, username, password, nickname, IFNULL(avatar,''), IFNULL(email,''), dept_id, status
		FROM system_users WHERE id = ? AND deleted = 0`, id))
}

func (m *MySQL) scanUser(row *sql.Row) (*User, error) {
	var user User
	var dept sql.NullInt64
	err := row.Scan(&user.ID, &user.TenantID, &user.Username, &user.Password, &user.Nickname, &user.Avatar, &user.Email, &dept, &user.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if dept.Valid {
		user.DeptID = &dept.Int64
	}
	return &user, nil
}

func (m *MySQL) TouchLogin(ctx context.Context, id int64, ip string, at int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET login_ip = ?, login_date = ? WHERE id = ?`,
		ip, time.UnixMilli(at), id)
	return err
}

func (m *MySQL) Client(ctx context.Context, clientID string) (*Client, error) {
	var client Client
	var accessSec, refreshSec int
	err := m.DB.QueryRowContext(ctx, `SELECT client_id, status, access_token_validity_seconds, refresh_token_validity_seconds
		FROM system_oauth2_client WHERE client_id = ? AND deleted = 0`, clientID).
		Scan(&client.ClientID, &client.Status, &accessSec, &refreshSec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	client.AccessTTL = time.Duration(accessSec) * time.Second
	client.RefreshTTL = time.Duration(refreshSec) * time.Second
	return &client, nil
}

// tokenExecer 让普通数据库句柄和事务共用同一条插入语句。
type tokenExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertRefresh(ctx context.Context, exec tokenExecer, token Token) error {
	scopes, err := encodeScopes(token.Scopes)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO system_oauth2_refresh_token
		(user_id, refresh_token, user_type, client_id, scopes, expires_time, deleted, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		token.UserID, token.RefreshToken, token.UserType, token.ClientID, scopes, token.ExpiresAt, token.TenantID)
	return err
}

func (m *MySQL) InsertAccess(ctx context.Context, token Token) error {
	return insertAccess(ctx, m.DB, token)
}

func insertAccess(ctx context.Context, exec tokenExecer, token Token) error {
	raw, err := json.Marshal(token.UserInfo)
	if err != nil {
		return err
	}
	scopes, err := encodeScopes(token.Scopes)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO system_oauth2_access_token
		(user_id, user_type, user_info, access_token, refresh_token, client_id, scopes, expires_time, deleted, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		token.UserID, token.UserType, string(raw), token.AccessToken, token.RefreshToken, token.ClientID, scopes, token.ExpiresAt, token.TenantID)
	return err
}

// InsertPair 确保访问令牌写入失败时不会留下可再次刷新的刷新令牌。
func (m *MySQL) InsertPair(ctx context.Context, refresh, access Token) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertRefresh(ctx, tx, refresh); err != nil {
		return err
	}
	if err := insertAccess(ctx, tx, access); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQL) FindAccess(ctx context.Context, accessToken string) (*Token, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT user_id, user_type, IFNULL(user_info,''), access_token, refresh_token, client_id, IFNULL(scopes,''), expires_time, tenant_id
		FROM system_oauth2_access_token WHERE access_token = ? AND deleted = 0`, accessToken)
	return scanToken(row)
}

func (m *MySQL) FindRefresh(ctx context.Context, refreshToken string) (*Token, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT user_id, user_type, '', '', refresh_token, client_id, IFNULL(scopes,''), expires_time, tenant_id
		FROM system_oauth2_refresh_token WHERE refresh_token = ? AND deleted = 0`, refreshToken)
	return scanToken(row)
}

func scanToken(row *sql.Row) (*Token, error) {
	var token Token
	var info, scopes string
	err := row.Scan(&token.UserID, &token.UserType, &info, &token.AccessToken, &token.RefreshToken, &token.ClientID, &scopes, &token.ExpiresAt, &token.TenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info != "" {
		if err := json.Unmarshal([]byte(info), &token.UserInfo); err != nil {
			return nil, err
		}
	}
	if scopes != "" {
		if err := json.Unmarshal([]byte(scopes), &token.Scopes); err != nil {
			return nil, err
		}
	}
	return &token, nil
}

// encodeScopes 保留 Java 数据表中 null 和空数组的区别。
func encodeScopes(scopes []string) (any, error) {
	if scopes == nil {
		return nil, nil
	}
	raw, err := json.Marshal(scopes)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// AccessesByUser 对齐 Java 的按用户撤销查询，租户条件阻止跨租户删除。
func (m *MySQL) AccessesByUser(ctx context.Context, tenantID, userID int64, userType int) ([]Token, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT access_token, refresh_token FROM system_oauth2_access_token
		WHERE tenant_id = ? AND user_id = ? AND user_type = ? AND deleted = 0`, tenantID, userID, userType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Token, 0)
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.AccessToken, &token.RefreshToken); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func (m *MySQL) DeleteAccessByRefresh(ctx context.Context, refreshToken string) ([]string, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT access_token FROM system_oauth2_access_token WHERE refresh_token = ? AND deleted = 0`, refreshToken)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_, err = m.DB.ExecContext(ctx, `UPDATE system_oauth2_access_token SET deleted = 1 WHERE refresh_token = ? AND deleted = 0`, refreshToken)
	return tokens, err
}

func (m *MySQL) DeleteRefresh(ctx context.Context, refreshToken string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_oauth2_refresh_token SET deleted = 1 WHERE refresh_token = ? AND deleted = 0`, refreshToken)
	return err
}

func (m *MySQL) DeleteAccess(ctx context.Context, accessToken string) (*Token, error) {
	token, err := m.FindAccess(ctx, accessToken)
	if err != nil || token == nil {
		return token, err
	}
	_, err = m.DB.ExecContext(ctx, `UPDATE system_oauth2_access_token SET deleted = 1 WHERE access_token = ? AND deleted = 0`, accessToken)
	return token, err
}

func (m *MySQL) AccessPage(ctx context.Context, tenantID int64, query AccessTokenQuery, now time.Time) (AccessTokenPage, error) {
	where := `WHERE deleted = 0 AND tenant_id = ? AND expires_time > ?`
	args := []any{tenantID, now}
	if query.UserID != nil {
		where += ` AND user_id = ?`
		args = append(args, *query.UserID)
	}
	if query.UserType != nil {
		where += ` AND user_type = ?`
		args = append(args, *query.UserType)
	}
	if query.ClientID != "" {
		where += ` AND client_id LIKE ?`
		args = append(args, "%"+query.ClientID+"%")
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_access_token `+where, args...).Scan(&total); err != nil {
		return AccessTokenPage{}, err
	}
	pageNo, pageSize := accessPageBounds(query)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, access_token, refresh_token, user_id, user_type, client_id,
		UNIX_TIMESTAMP(create_time)*1000, UNIX_TIMESTAMP(expires_time)*1000
		FROM system_oauth2_access_token `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return AccessTokenPage{}, err
	}
	defer rows.Close()
	list := make([]AccessTokenItem, 0)
	for rows.Next() {
		var item AccessTokenItem
		if err := rows.Scan(&item.ID, &item.AccessToken, &item.RefreshToken, &item.UserID, &item.UserType, &item.ClientID, &item.CreateTime, &item.ExpiresTime); err != nil {
			return AccessTokenPage{}, err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return AccessTokenPage{}, err
	}
	return AccessTokenPage{List: list, Total: total}, nil
}

func (m *MySQL) RolesByUser(ctx context.Context, userID int64) ([]Role, error) {
	// 三张表的 tenant_id 必须一致。即使库中留有错误授权关系，也不能把其他租户的超级管理员角色授给当前用户。
	rows, err := m.DB.QueryContext(ctx, `SELECT r.id, r.code, r.status
		FROM system_role r
		JOIN system_user_role ur ON ur.role_id = r.id AND ur.tenant_id = r.tenant_id AND ur.deleted = 0
		JOIN system_users u ON u.id = ur.user_id AND u.tenant_id = ur.tenant_id AND u.deleted = 0
		WHERE ur.user_id = ? AND r.deleted = 0`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Status); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (m *MySQL) MenusByRole(ctx context.Context, roleIDs []int64, all bool) ([]Menu, error) {
	query := `SELECT id, parent_id, name, IFNULL(permission,''), type, sort, IFNULL(path,''), IFNULL(icon,''), IFNULL(component,''), IFNULL(component_name,''), status, visible+0, keep_alive+0, always_show+0
		FROM system_menu WHERE deleted = 0 AND status = 0`
	var args []any
	if !all {
		if len(roleIDs) == 0 {
			return nil, nil
		}
		query += ` AND id IN (SELECT rm.menu_id FROM system_role_menu rm
			JOIN system_role r ON r.id=rm.role_id AND r.tenant_id=rm.tenant_id AND r.deleted=0
			WHERE rm.deleted=0 AND rm.role_id IN (`
		for i, id := range roleIDs {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, id)
		}
		query += `))`
	}
	query += ` ORDER BY sort, id`
	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var menus []Menu
	for rows.Next() {
		var menu Menu
		var visible, keepAlive, alwaysShow int
		if err := rows.Scan(&menu.ID, &menu.ParentID, &menu.Name, &menu.Permission, &menu.Type, &menu.Sort, &menu.Path, &menu.Icon, &menu.Component, &menu.ComponentName, &menu.Status, &visible, &keepAlive, &alwaysShow); err != nil {
			return nil, err
		}
		menu.Visible = visible != 0
		menu.KeepAlive = keepAlive != 0
		menu.AlwaysShow = alwaysShow != 0
		menus = append(menus, menu)
	}
	return menus, rows.Err()
}

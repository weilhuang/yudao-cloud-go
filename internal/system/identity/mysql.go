package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// MySQL 读写 OAuth2 客户端和社交账号。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) ClientByID(ctx context.Context, id int64) (*OAuthClient, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT id, client_id, secret, name, IFNULL(logo,''), IFNULL(description,''), status,
		access_token_validity_seconds, refresh_token_validity_seconds, IFNULL(redirect_uris,'[]'), IFNULL(authorized_grant_types,'[]'),
		IFNULL(scopes,'[]'), IFNULL(auto_approve_scopes,'[]'), IFNULL(authorities,'[]'), IFNULL(resource_ids,'[]'), IFNULL(additional_information,''),
		IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM system_oauth2_client WHERE id=? AND deleted=0`, id)
	return scanClient(row)
}

func (m *MySQL) ClientByClientID(ctx context.Context, clientID string) (*OAuthClient, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT id, client_id, secret, name, IFNULL(logo,''), IFNULL(description,''), status,
		access_token_validity_seconds, refresh_token_validity_seconds, IFNULL(redirect_uris,'[]'), IFNULL(authorized_grant_types,'[]'),
		IFNULL(scopes,'[]'), IFNULL(auto_approve_scopes,'[]'), IFNULL(authorities,'[]'), IFNULL(resource_ids,'[]'), IFNULL(additional_information,''),
		IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM system_oauth2_client WHERE client_id=? AND deleted=0`, clientID)
	return scanClient(row)
}

func scanClient(row *sql.Row) (*OAuthClient, error) {
	var item OAuthClient
	var redirect, grants, scopes, auto, authz, resources string
	err := row.Scan(&item.ID, &item.ClientID, &item.Secret, &item.Name, &item.Logo, &item.Description, &item.Status,
		&item.AccessTokenValiditySeconds, &item.RefreshTokenValiditySeconds, &redirect, &grants, &scopes, &auto, &authz, &resources,
		&item.AdditionalInformation, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.RedirectURIs = decodeList(redirect)
	item.AuthorizedGrantTypes = decodeList(grants)
	item.Scopes = decodeList(scopes)
	item.AutoApproveScopes = decodeList(auto)
	item.Authorities = decodeList(authz)
	item.ResourceIDs = decodeList(resources)
	return &item, nil
}

func (m *MySQL) ClientIDTaken(ctx context.Context, clientID string, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_client WHERE deleted=0 AND client_id=? AND id<>?`, clientID, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateClient(ctx context.Context, item OAuthClient) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_oauth2_client
		(client_id, secret, name, logo, description, status, access_token_validity_seconds, refresh_token_validity_seconds,
		 redirect_uris, authorized_grant_types, scopes, auto_approve_scopes, authorities, resource_ids, additional_information, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.ClientID, item.Secret, item.Name, item.Logo, item.Description, item.Status, item.AccessTokenValiditySeconds, item.RefreshTokenValiditySeconds,
		encodeList(item.RedirectURIs), encodeList(item.AuthorizedGrantTypes), encodeList(item.Scopes), encodeList(item.AutoApproveScopes),
		encodeList(item.Authorities), encodeList(item.ResourceIDs), item.AdditionalInformation, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateClient(ctx context.Context, item OAuthClient) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_oauth2_client SET client_id=?, secret=?, name=?, logo=?, description=?, status=?,
		access_token_validity_seconds=?, refresh_token_validity_seconds=?, redirect_uris=?, authorized_grant_types=?, scopes=?,
		auto_approve_scopes=?, authorities=?, resource_ids=?, additional_information=? WHERE id=? AND deleted=0`,
		item.ClientID, item.Secret, item.Name, item.Logo, item.Description, item.Status, item.AccessTokenValiditySeconds, item.RefreshTokenValiditySeconds,
		encodeList(item.RedirectURIs), encodeList(item.AuthorizedGrantTypes), encodeList(item.Scopes), encodeList(item.AutoApproveScopes),
		encodeList(item.Authorities), encodeList(item.ResourceIDs), item.AdditionalInformation, item.ID)
	return err
}

func (m *MySQL) DeleteClient(ctx context.Context, id int64) error {
	return m.deleteClientIDs(ctx, []int64{id})
}

func (m *MySQL) DeleteClientList(ctx context.Context, ids []int64) error {
	return m.deleteClientIDs(ctx, ids)
}

func (m *MySQL) deleteClientIDs(ctx context.Context, ids []int64) error {
	unique, err := uniqueIDs(ids)
	if err != nil || len(unique) == 0 {
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}
	_, err = m.DB.ExecContext(ctx, `UPDATE system_oauth2_client SET deleted=1 WHERE deleted=0 AND id IN (`+marks+`)`, args...)
	return err
}

func (m *MySQL) ClientPage(ctx context.Context, pageNo, pageSize int, name string, status *int) (Page[OAuthClient], error) {
	where := `WHERE deleted=0`
	var args []any
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_client `+where, args...).Scan(&total); err != nil {
		return Page[OAuthClient]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, client_id, secret, name, status, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_oauth2_client `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[OAuthClient]{}, err
	}
	defer rows.Close()
	list := make([]OAuthClient, 0)
	for rows.Next() {
		var item OAuthClient
		if err := rows.Scan(&item.ID, &item.ClientID, &item.Secret, &item.Name, &item.Status, &item.CreateTime); err != nil {
			return Page[OAuthClient]{}, err
		}
		list = append(list, item)
	}
	return Page[OAuthClient]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) SocialByID(ctx context.Context, tenantID, id int64) (*SocialClient, error) {
	return scanSocial(m.DB.QueryRowContext(ctx, socialSelect+` WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID))
}

func (m *MySQL) SocialByType(ctx context.Context, tenantID int64, socialType, userType int) (*SocialClient, error) {
	return scanSocial(m.DB.QueryRowContext(ctx, socialSelect+` WHERE tenant_id=? AND social_type=? AND user_type=? AND deleted=0`, tenantID, socialType, userType))
}

const socialSelect = `SELECT id, name, social_type, user_type, client_id, client_secret, IFNULL(agent_id,''), IFNULL(public_key,''), status, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_social_client`

func scanSocial(row *sql.Row) (*SocialClient, error) {
	var item SocialClient
	err := row.Scan(&item.ID, &item.Name, &item.SocialType, &item.UserType, &item.ClientID, &item.ClientSecret, &item.AgentID, &item.PublicKey, &item.Status, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MySQL) SocialTaken(ctx context.Context, tenantID int64, socialType, userType int, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_social_client WHERE deleted=0 AND tenant_id=? AND social_type=? AND user_type=? AND id<>?`, tenantID, socialType, userType, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateSocial(ctx context.Context, tenantID int64, item SocialClient) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_social_client (name, social_type, user_type, client_id, client_secret, agent_id, public_key, status, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Name, item.SocialType, item.UserType, item.ClientID, item.ClientSecret, item.AgentID, item.PublicKey, item.Status, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateSocial(ctx context.Context, tenantID int64, item SocialClient) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_social_client SET name=?, social_type=?, user_type=?, client_id=?, client_secret=?, agent_id=?, public_key=?, status=? WHERE id=? AND tenant_id=? AND deleted=0`,
		item.Name, item.SocialType, item.UserType, item.ClientID, item.ClientSecret, item.AgentID, item.PublicKey, item.Status, item.ID, tenantID)
	return err
}

func (m *MySQL) DeleteSocial(ctx context.Context, tenantID, id int64) error {
	return m.deleteSocialIDs(ctx, tenantID, []int64{id})
}

func (m *MySQL) DeleteSocialList(ctx context.Context, tenantID int64, ids []int64) error {
	return m.deleteSocialIDs(ctx, tenantID, ids)
}

func (m *MySQL) deleteSocialIDs(ctx context.Context, tenantID int64, ids []int64) error {
	unique, err := uniqueIDs(ids)
	if err != nil || len(unique) == 0 {
		return err
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, 0, len(unique)+1)
	args = append(args, tenantID)
	for _, id := range unique {
		args = append(args, id)
	}
	_, err = m.DB.ExecContext(ctx, `UPDATE system_social_client SET deleted=1 WHERE deleted=0 AND tenant_id=? AND id IN (`+marks+`)`, args...)
	return err
}

func uniqueIDs(ids []int64) ([]int64, error) {
	if len(ids) > 1000 {
		return nil, &Error{Code: 400, Msg: "请求参数过多"}
	}
	seen := map[int64]struct{}{}
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	return unique, nil
}

func (m *MySQL) SocialPage(ctx context.Context, tenantID int64, pageNo, pageSize int, name string, socialType *int) (Page[SocialClient], error) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if socialType != nil {
		where += ` AND social_type=?`
		args = append(args, *socialType)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_social_client `+where, args...).Scan(&total); err != nil {
		return Page[SocialClient]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, socialSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[SocialClient]{}, err
	}
	defer rows.Close()
	list := make([]SocialClient, 0)
	for rows.Next() {
		var item SocialClient
		if err := rows.Scan(&item.ID, &item.Name, &item.SocialType, &item.UserType, &item.ClientID, &item.ClientSecret, &item.AgentID, &item.PublicKey, &item.Status, &item.CreateTime); err != nil {
			return Page[SocialClient]{}, err
		}
		list = append(list, item)
	}
	return Page[SocialClient]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) SocialUserByOpenID(ctx context.Context, tenantID int64, socialType int, openID string) (*SocialUser, error) {
	var item SocialUser
	err := m.DB.QueryRowContext(ctx, `SELECT id, type, openid, IFNULL(nickname,''), IFNULL(avatar,'') FROM system_social_user WHERE deleted=0 AND tenant_id=? AND type=? AND openid=?`, tenantID, socialType, openID).
		Scan(&item.ID, &item.Type, &item.OpenID, &item.Nickname, &item.Avatar)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MySQL) SocialUserByCodeState(ctx context.Context, tenantID int64, socialType int, code, state string) (*SocialUser, error) {
	var item SocialUser
	err := m.DB.QueryRowContext(ctx, `SELECT id, type, openid, IFNULL(nickname,''), IFNULL(avatar,'')
		FROM system_social_user WHERE deleted=0 AND tenant_id=? AND type=? AND code=? AND state=?`, tenantID, socialType, code, state).
		Scan(&item.ID, &item.Type, &item.OpenID, &item.Nickname, &item.Avatar)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MySQL) SocialUserByID(ctx context.Context, tenantID, id int64) (*SocialUserDetail, error) {
	var item SocialUserDetail
	var created, updated sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT id, type, openid, IFNULL(token,''), IFNULL(raw_token_info,''), IFNULL(nickname,''), IFNULL(avatar,''),
		IFNULL(raw_user_info,''), IFNULL(code,''), IFNULL(state,''), UNIX_TIMESTAMP(create_time)*1000, UNIX_TIMESTAMP(update_time)*1000
		FROM system_social_user WHERE deleted=0 AND tenant_id=? AND id=?`, tenantID, id).
		Scan(&item.ID, &item.Type, &item.OpenID, &item.Token, &item.RawTokenInfo, &item.Nickname, &item.Avatar,
			&item.RawUserInfo, &item.Code, &item.State, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	if updated.Valid {
		item.UpdateTime = updated.Int64
	}
	return &item, nil
}

func (m *MySQL) BindUserID(ctx context.Context, tenantID int64, socialType int, openID string) (int64, bool, error) {
	var userID int64
	err := m.DB.QueryRowContext(ctx, `SELECT b.user_id FROM system_social_user_bind b
		JOIN system_social_user u ON u.id=b.social_user_id AND u.deleted=0
		WHERE b.deleted=0 AND b.tenant_id=? AND b.social_type=? AND u.openid=?`, tenantID, socialType, openID).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}

func (m *MySQL) SaveSocialUser(ctx context.Context, tenantID int64, item SocialUser) (int64, error) {
	current, err := m.SocialUserByOpenID(ctx, tenantID, item.Type, item.OpenID)
	if err != nil {
		return 0, err
	}
	if current != nil {
		query := `UPDATE system_social_user SET nickname=?, avatar=?`
		args := []any{item.Nickname, item.Avatar}
		if item.Code != "" {
			query += `, code=?, state=?`
			args = append(args, item.Code, item.State)
		}
		query += ` WHERE id=?`
		args = append(args, current.ID)
		_, err = m.DB.ExecContext(ctx, query, args...)
		return current.ID, err
	}
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_social_user (type, openid, nickname, avatar, token, raw_token_info, raw_user_info, code, state, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, '', '', '', ?, ?, ?, 0, ?)`, item.Type, item.OpenID, item.Nickname, item.Avatar, item.Code, item.State, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) Bind(ctx context.Context, tenantID, userID int64, userType, socialType int, socialUserID int64) error {
	_, err := m.DB.ExecContext(ctx, `INSERT INTO system_social_user_bind (user_id, user_type, social_type, social_user_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, userType, socialType, socialUserID, tenantID, time.Now())
	return err
}

func (m *MySQL) Rebind(ctx context.Context, tenantID, userID int64, userType, socialType int, socialUserID int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE system_social_user_bind SET deleted=1
		WHERE deleted=0 AND tenant_id=? AND user_type=? AND social_user_id=?`, tenantID, userType, socialUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_social_user_bind SET deleted=1
		WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND social_type=?`, tenantID, userID, userType, socialType); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO system_social_user_bind
		(user_id, user_type, social_type, social_user_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, userType, socialType, socialUserID, tenantID, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQL) Unbind(ctx context.Context, tenantID, userID int64, userType, socialType int) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_social_user_bind SET deleted=1 WHERE tenant_id=? AND user_id=? AND user_type=? AND social_type=? AND deleted=0`, tenantID, userID, userType, socialType)
	return err
}

func (m *MySQL) SocialByBind(ctx context.Context, tenantID, userID int64, userType, socialType int) (*SocialUser, error) {
	var item SocialUser
	err := m.DB.QueryRowContext(ctx, `SELECT u.id, u.type, u.openid, IFNULL(u.nickname,''), IFNULL(u.avatar,''), b.user_id
		FROM system_social_user_bind b JOIN system_social_user u ON u.id=b.social_user_id AND u.deleted=0
		WHERE b.deleted=0 AND b.tenant_id=? AND b.user_id=? AND b.user_type=? AND b.social_type=? LIMIT 1`,
		tenantID, userID, userType, socialType).Scan(&item.ID, &item.Type, &item.OpenID, &item.Nickname, &item.Avatar, &item.UserID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MySQL) BoundUserID(ctx context.Context, tenantID int64, userType int, socialUserID int64) (*int64, error) {
	var userID int64
	err := m.DB.QueryRowContext(ctx, `SELECT user_id FROM system_social_user_bind
		WHERE deleted=0 AND tenant_id=? AND user_type=? AND social_user_id=? LIMIT 1`, tenantID, userType, socialUserID).Scan(&userID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &userID, nil
}

func (m *MySQL) BindList(ctx context.Context, tenantID, userID int64) ([]SocialUser, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT u.id, u.type, u.openid, IFNULL(u.nickname,''), IFNULL(u.avatar,''), b.user_id
		FROM system_social_user_bind b JOIN system_social_user u ON u.id=b.social_user_id AND u.deleted=0
		WHERE b.deleted=0 AND b.tenant_id=? AND b.user_id=?`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SocialUser
	for rows.Next() {
		var item SocialUser
		if err := rows.Scan(&item.ID, &item.Type, &item.OpenID, &item.Nickname, &item.Avatar, &item.UserID); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) SocialUserPage(ctx context.Context, tenantID int64, pageNo, pageSize int, openID string) (Page[SocialUser], error) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if openID != "" {
		where += ` AND openid LIKE ?`
		args = append(args, "%"+openID+"%")
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_social_user `+where, args...).Scan(&total); err != nil {
		return Page[SocialUser]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, type, openid, IFNULL(nickname,''), IFNULL(avatar,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_social_user `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[SocialUser]{}, err
	}
	defer rows.Close()
	list := make([]SocialUser, 0)
	for rows.Next() {
		var item SocialUser
		if err := rows.Scan(&item.ID, &item.Type, &item.OpenID, &item.Nickname, &item.Avatar, &item.CreateTime); err != nil {
			return Page[SocialUser]{}, err
		}
		list = append(list, item)
	}
	return Page[SocialUser]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) Approves(ctx context.Context, tenantID, userID int64, userType int, clientID string, now time.Time) ([]Approve, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT scope, approved FROM system_oauth2_approve
		WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND client_id=? AND expires_time>?`,
		tenantID, userID, userType, clientID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Approve, 0)
	for rows.Next() {
		var item Approve
		var approved []byte
		if err := rows.Scan(&item.Scope, &approved); err != nil {
			return nil, err
		}
		item.Approved = len(approved) > 0 && approved[0] == 1
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) SaveApprove(ctx context.Context, tenantID, userID int64, userType int, clientID, scope string, approved bool, expires time.Time) error {
	flag := 0
	if approved {
		flag = 1
	}
	res, err := m.DB.ExecContext(ctx, `UPDATE system_oauth2_approve SET approved=?, expires_time=?
		WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND client_id=? AND scope=?`,
		flag, expires, tenantID, userID, userType, clientID, scope)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	_, err = m.DB.ExecContext(ctx, `INSERT INTO system_oauth2_approve
		(user_id, user_type, client_id, scope, approved, expires_time, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`, userID, userType, clientID, scope, flag, expires, tenantID, time.Now())
	return err
}

func (m *MySQL) InsertCode(ctx context.Context, tenantID, userID int64, userType int, clientID string, scopes []string, redirectURI, state string, expires time.Time) (string, error) {
	code, err := randomCode()
	if err != nil {
		return "", err
	}
	if state == "" {
		state = ""
	}
	_, err = m.DB.ExecContext(ctx, `INSERT INTO system_oauth2_code
		(user_id, user_type, code, client_id, scopes, expires_time, redirect_uri, state, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		userID, userType, code, clientID, encodeList(scopes), expires, redirectURI, state, tenantID, time.Now())
	if err != nil {
		return "", err
	}
	return code, nil
}

func (m *MySQL) ConsumeCode(ctx context.Context, code string, now time.Time) (*AuthCode, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var item AuthCode
	var scopes string
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT user_id, user_type, tenant_id, client_id, IFNULL(scopes,'[]'), redirect_uri, IFNULL(state,''), expires_time
		FROM system_oauth2_code WHERE code=? AND deleted=0 FOR UPDATE`, code).
		Scan(&item.UserID, &item.UserType, &item.TenantID, &item.ClientID, &scopes, &item.RedirectURI, &item.State, &expires)
	if err == sql.ErrNoRows {
		return nil, &Error{Code: 1_002_022_000, Msg: "code 不存在"}
	}
	if err != nil {
		return nil, err
	}
	if !expires.After(now) {
		return nil, &Error{Code: 1_002_022_001, Msg: "code 已过期"}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_oauth2_code SET deleted=1 WHERE code=? AND deleted=0`, code); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	item.Scopes = decodeList(scopes)
	return &item, nil
}

func encodeList(items []string) string {
	if items == nil {
		items = []string{}
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}

func decodeList(raw string) []string {
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil || items == nil {
		return []string{}
	}
	return items
}

func pageOf(pageNo, pageSize int) (int, int) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageNo, pageSize
}

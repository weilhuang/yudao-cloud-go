package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// MySQL 同时实现 Reader 和 UserWriter。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) DictSimple(ctx context.Context) ([]DictItem, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT dict_type, value, label, IFNULL(color_type,''), IFNULL(css_class,'')
		FROM system_dict_data WHERE deleted = 0 AND status = 0 ORDER BY dict_type, sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DictItem
	for rows.Next() {
		var item DictItem
		if err := rows.Scan(&item.DictType, &item.Value, &item.Label, &item.ColorType, &item.CSSClass); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) DictEnabledByType(ctx context.Context, dictType string) ([]DictData, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, label, value, dict_type
		FROM system_dict_data WHERE deleted=0 AND status=0 AND dict_type=? ORDER BY sort, id`, dictType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []DictData{}
	for rows.Next() {
		var item DictData
		if err := rows.Scan(&item.ID, &item.Label, &item.Value, &item.DictType); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) DeptList(ctx context.Context, tenantID int64) ([]Dept, error) {
	rows, err := m.DB.QueryContext(ctx, deptSelect+` WHERE deleted = 0 AND tenant_id = ? ORDER BY sort, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Dept
	for rows.Next() {
		dept, err := scanDept(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, dept)
	}
	return list, rows.Err()
}

const deptSelect = `SELECT id, name, parent_id, sort, leader_user_id, IFNULL(phone,''), IFNULL(email,''), status,
	UNIX_TIMESTAMP(create_time)*1000 FROM system_dept`

// DeptGet 与列表共用扫描器；不存在、已软删或属于其他租户时都返回 nil。
func (m *MySQL) DeptGet(ctx context.Context, tenantID, id int64) (*Dept, error) {
	dept, err := scanDept(m.DB.QueryRowContext(ctx, deptSelect+` WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &dept, nil
}

func scanDept(row interface{ Scan(...any) error }) (Dept, error) {
	var dept Dept
	var leader, created sql.NullInt64
	err := row.Scan(&dept.ID, &dept.Name, &dept.ParentID, &dept.Sort, &leader, &dept.Phone, &dept.Email, &dept.Status, &created)
	if err != nil {
		return Dept{}, err
	}
	if leader.Valid {
		dept.LeaderUserID = &leader.Int64
	}
	if created.Valid {
		dept.CreateTime = created.Int64
	}
	return dept, nil
}

func (m *MySQL) UserSimple(ctx context.Context, tenantID int64) ([]UserSimple, error) {
	clause, scopeArgs := accessSQL(accessFrom(ctx))
	args := append([]any{tenantID}, scopeArgs...)
	rows, err := m.DB.QueryContext(ctx, `SELECT u.id, u.nickname, IFNULL(u.avatar,''), u.sex, u.dept_id, IFNULL(d.name,'')
		FROM system_users u LEFT JOIN system_dept d ON d.id = u.dept_id AND d.deleted = 0
		WHERE u.deleted = 0 AND u.status = 0 AND u.tenant_id = ?`+clause+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []UserSimple
	for rows.Next() {
		var user UserSimple
		var sex sql.NullInt64
		var dept sql.NullInt64
		if err := rows.Scan(&user.ID, &user.Nickname, &user.Avatar, &sex, &dept, &user.DeptName); err != nil {
			return nil, err
		}
		if sex.Valid {
			v := int(sex.Int64)
			user.Sex = &v
		}
		if dept.Valid {
			user.DeptID = &dept.Int64
		}
		list = append(list, user)
	}
	return list, rows.Err()
}

func (m *MySQL) UserPage(ctx context.Context, tenantID int64, query UserQuery) (Page[UserDetail], error) {
	query, err := m.expandUserQuery(ctx, tenantID, query)
	if err != nil {
		return Page[UserDetail]{}, err
	}
	where, args := userFilter(tenantID, query)
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users u `+where, args...).Scan(&total); err != nil {
		return Page[UserDetail]{}, err
	}
	pageNo, pageSize := query.PageNo, query.PageSize
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT u.id, u.username, u.nickname, IFNULL(u.remark,''), u.dept_id, IFNULL(d.name,''), u.post_ids,
		IFNULL(u.email,''), IFNULL(u.mobile,''), u.sex, IFNULL(u.avatar,''), u.status, IFNULL(u.login_ip,''),
		UNIX_TIMESTAMP(u.login_date)*1000, UNIX_TIMESTAMP(u.create_time)*1000
		FROM system_users u LEFT JOIN system_dept d ON d.id = u.dept_id AND d.deleted = 0 `+where+
		` ORDER BY u.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[UserDetail]{}, err
	}
	defer rows.Close()
	list := make([]UserDetail, 0)
	for rows.Next() {
		var user UserDetail
		var dept sql.NullInt64
		var posts sql.NullString
		var sex sql.NullInt64
		var login sql.NullInt64
		var created sql.NullInt64
		if err := rows.Scan(&user.ID, &user.Username, &user.Nickname, &user.Remark, &dept, &user.DeptName, &posts, &user.Email, &user.Mobile, &sex, &user.Avatar, &user.Status, &user.LoginIP, &login, &created); err != nil {
			return Page[UserDetail]{}, err
		}
		if posts.Valid {
			if err := json.Unmarshal([]byte(posts.String), &user.PostIDs); err != nil {
				return Page[UserDetail]{}, err
			}
		}
		if dept.Valid {
			user.DeptID = &dept.Int64
		}
		if sex.Valid {
			v := int(sex.Int64)
			user.Sex = &v
		}
		if login.Valid && login.Int64 > 0 {
			user.LoginDate = &login.Int64
		}
		if created.Valid {
			user.CreateTime = created.Int64
		}
		list = append(list, user)
	}
	return Page[UserDetail]{List: list, Total: total}, rows.Err()
}

func userFilter(tenantID int64, query UserQuery) (string, []any) {
	where := `WHERE u.deleted = 0 AND u.tenant_id = ?`
	args := []any{tenantID}
	if query.Username != "" {
		where += ` AND u.username LIKE ?`
		args = append(args, "%"+query.Username+"%")
	}
	if query.Mobile != "" {
		where += ` AND u.mobile LIKE ?`
		args = append(args, "%"+query.Mobile+"%")
	}
	if query.Status != nil {
		where += ` AND u.status = ?`
		args = append(args, *query.Status)
	}
	deptIDs := query.DeptIDs
	if query.DeptID != nil {
		deptIDs = []int64{*query.DeptID}
	}
	if len(deptIDs) > 0 {
		where += ` AND u.dept_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(deptIDs)), ",") + `)`
		for _, id := range deptIDs {
			args = append(args, id)
		}
	}
	if query.RoleID != nil {
		where += ` AND EXISTS (SELECT 1 FROM system_user_role ur WHERE ur.user_id=u.id AND ur.tenant_id=? AND ur.deleted=0 AND ur.role_id=?)`
		args = append(args, tenantID, *query.RoleID)
	}
	if query.CreatedFrom != nil {
		where += ` AND u.create_time >= ?`
		args = append(args, *query.CreatedFrom)
	}
	if query.CreatedTo != nil {
		where += ` AND u.create_time <= ?`
		args = append(args, *query.CreatedTo)
	}
	clause, scopeArgs := accessSQL(query.Access)
	where += clause
	args = append(args, scopeArgs...)
	return where, args
}

func accessSQL(scope *UserAccess) (string, []any) {
	if scope == nil || scope.All {
		return "", nil
	}
	if len(scope.DeptIDs) == 0 && !scope.Self {
		return " AND 1=0", nil
	}
	if len(scope.DeptIDs) > 0 && scope.Self {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(scope.DeptIDs)), ",")
		args := make([]any, 0, len(scope.DeptIDs)+1)
		for _, id := range scope.DeptIDs {
			args = append(args, id)
		}
		args = append(args, scope.UserID)
		return " AND (u.dept_id IN (" + marks + ") OR u.id=?)", args
	}
	if len(scope.DeptIDs) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(scope.DeptIDs)), ",")
		args := make([]any, len(scope.DeptIDs))
		for i, id := range scope.DeptIDs {
			args[i] = id
		}
		return " AND u.dept_id IN (" + marks + ")", args
	}
	return " AND u.id=?", []any{scope.UserID}
}

func (m *MySQL) UserGet(ctx context.Context, tenantID, id int64) (*UserDetail, error) {
	clause, scopeArgs := accessSQL(accessFrom(ctx))
	args := append([]any{tenantID, id}, scopeArgs...)
	row := m.DB.QueryRowContext(ctx, `SELECT u.id, u.username, u.nickname, IFNULL(u.remark,''), u.dept_id, IFNULL(d.name,''), u.post_ids,
		IFNULL(u.email,''), IFNULL(u.mobile,''), u.sex, IFNULL(u.avatar,''), u.status, IFNULL(u.login_ip,''),
		UNIX_TIMESTAMP(u.login_date)*1000, UNIX_TIMESTAMP(u.create_time)*1000
		FROM system_users u LEFT JOIN system_dept d ON d.id = u.dept_id AND d.deleted = 0
		WHERE u.deleted = 0 AND u.tenant_id = ? AND u.id = ?`+clause, args...)
	var user UserDetail
	var dept sql.NullInt64
	var posts sql.NullString
	var sex sql.NullInt64
	var login sql.NullInt64
	var created sql.NullInt64
	err := row.Scan(&user.ID, &user.Username, &user.Nickname, &user.Remark, &dept, &user.DeptName, &posts, &user.Email, &user.Mobile, &sex, &user.Avatar, &user.Status, &user.LoginIP, &login, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if posts.Valid {
		if err := json.Unmarshal([]byte(posts.String), &user.PostIDs); err != nil {
			return nil, err
		}
	}
	if dept.Valid {
		user.DeptID = &dept.Int64
	}
	if sex.Valid {
		v := int(sex.Int64)
		user.Sex = &v
	}
	if login.Valid && login.Int64 > 0 {
		user.LoginDate = &login.Int64
	}
	if created.Valid {
		user.CreateTime = created.Int64
	}
	return &user, nil
}

func (m *MySQL) MenuSimple(ctx context.Context) ([]MenuSimple, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, parent_id, type FROM system_menu WHERE deleted = 0 AND status = 0 ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []MenuSimple
	for rows.Next() {
		var menu MenuSimple
		if err := rows.Scan(&menu.ID, &menu.Name, &menu.ParentID, &menu.Type); err != nil {
			return nil, err
		}
		list = append(list, menu)
	}
	return list, rows.Err()
}

func (m *MySQL) MenuList(ctx context.Context, name string, status *int) ([]MenuDetail, error) {
	query := `SELECT id, name, IFNULL(permission,''), type, sort, parent_id, IFNULL(path,''), IFNULL(icon,''), IFNULL(component,''), IFNULL(component_name,''), status, visible+0, keep_alive+0, always_show+0, UNIX_TIMESTAMP(create_time)*1000
		FROM system_menu WHERE deleted = 0`
	var args []any
	if name != "" {
		query += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if status != nil {
		query += ` AND status = ?`
		args = append(args, *status)
	}
	query += ` ORDER BY sort, id`
	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []MenuDetail
	for rows.Next() {
		var menu MenuDetail
		var visible, keepAlive, alwaysShow int
		if err := rows.Scan(&menu.ID, &menu.Name, &menu.Permission, &menu.Type, &menu.Sort, &menu.ParentID, &menu.Path, &menu.Icon, &menu.Component, &menu.ComponentName, &menu.Status, &visible, &keepAlive, &alwaysShow, &menu.CreateTime); err != nil {
			return nil, err
		}
		menu.Visible = visible != 0
		menu.KeepAlive = keepAlive != 0
		menu.AlwaysShow = alwaysShow != 0
		list = append(list, menu)
	}
	return list, rows.Err()
}

func (m *MySQL) RolePage(ctx context.Context, tenantID int64, pageNo, pageSize int, name, code string, status *int) (Page[RoleDetail], error) {
	where := `WHERE deleted = 0 AND tenant_id = ?`
	args := []any{tenantID}
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if code != "" {
		where += ` AND code LIKE ?`
		args = append(args, "%"+code+"%")
	}
	if status != nil {
		where += ` AND status = ?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_role `+where, args...).Scan(&total); err != nil {
		return Page[RoleDetail]{}, err
	}
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code, sort, status, type, IFNULL(remark,''), data_scope, IFNULL(data_scope_dept_ids,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_role `+where+` ORDER BY sort, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[RoleDetail]{}, err
	}
	defer rows.Close()
	list := make([]RoleDetail, 0)
	for rows.Next() {
		role, err := scanRoleDetail(rows)
		if err != nil {
			return Page[RoleDetail]{}, err
		}
		list = append(list, role)
	}
	return Page[RoleDetail]{List: list, Total: total}, rows.Err()
}

// RoleGet 与分页共用一套扫描逻辑，确保 dataScopeDeptIds 不会在详情中遗漏。
func (m *MySQL) RoleGet(ctx context.Context, tenantID, id int64) (*RoleDetail, error) {
	role, err := scanRoleDetail(m.DB.QueryRowContext(ctx, `SELECT id, name, code, sort, status, type,
		IFNULL(remark,''), data_scope, IFNULL(data_scope_dept_ids,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_role WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &role, nil
}

type roleScanner interface {
	Scan(dest ...any) error
}

func scanRoleDetail(row roleScanner) (RoleDetail, error) {
	var role RoleDetail
	var encoded string
	var created sql.NullInt64
	if err := row.Scan(&role.ID, &role.Name, &role.Code, &role.Sort, &role.Status, &role.Type,
		&role.Remark, &role.DataScope, &encoded, &created); err != nil {
		return RoleDetail{}, err
	}
	if encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &role.DataScopeDeptIDs); err != nil {
			return RoleDetail{}, err
		}
	}
	if created.Valid {
		role.CreateTime = created.Int64
	}
	return role, nil
}

func (m *MySQL) PostSimple(ctx context.Context, tenantID int64) ([]PostSimple, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name FROM system_post WHERE deleted = 0 AND status = 0 AND tenant_id = ? ORDER BY sort, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []PostSimple
	for rows.Next() {
		var post PostSimple
		if err := rows.Scan(&post.ID, &post.Name); err != nil {
			return nil, err
		}
		list = append(list, post)
	}
	return list, rows.Err()
}

// PostsByIDs 按请求中的岗位编号返回名称，不过滤停用岗位，也不补上其他租户或已删除的岗位。
func (m *MySQL) PostsByIDs(ctx context.Context, tenantID int64, ids []int64) ([]PostSimple, error) {
	if len(ids) == 0 {
		return []PostSimple{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name FROM system_post WHERE deleted=0 AND tenant_id=? AND id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		found[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ordered := make([]PostSimple, 0, len(ids))
	for _, id := range ids {
		if name, ok := found[id]; ok {
			ordered = append(ordered, PostSimple{ID: id, Name: name})
		}
	}
	return ordered, nil
}

func (m *MySQL) RoleSimple(ctx context.Context, tenantID int64) ([]RoleSimple, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name FROM system_role WHERE deleted = 0 AND status = 0 AND tenant_id = ? ORDER BY sort, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RoleSimple
	for rows.Next() {
		var role RoleSimple
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			return nil, err
		}
		list = append(list, role)
	}
	return list, rows.Err()
}

func (m *MySQL) UsernameTaken(ctx context.Context, tenantID int64, username string, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users WHERE deleted = 0 AND tenant_id = ? AND username = ? AND id <> ?`,
		tenantID, username, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateUser(ctx context.Context, tenantID int64, user UserSave, passwordHash string) (int64, error) {
	sex := 0
	if user.Sex != nil {
		sex = *user.Sex
	}
	posts, err := userPostJSON(user.PostIDs)
	if err != nil {
		return 0, err
	}
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO system_users
		(username, password, nickname, remark, dept_id, post_ids, email, mobile, sex, avatar, status, deleted, tenant_id, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?, ?)`,
		user.Username, passwordHash, user.Nickname, user.Remark, user.DeptID, posts, user.Email, user.Mobile, sex, user.Avatar, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, postID := range uniquePostIDs(user.PostIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_user_post (user_id, post_id, tenant_id, deleted) VALUES (?, ?, ?, 0)`, id, postID, tenantID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (m *MySQL) UpdateUser(ctx context.Context, tenantID int64, user UserSave) error {
	posts, err := userPostJSON(user.PostIDs)
	if err != nil {
		return err
	}
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 锁住用户主行，使同一用户的两次岗位修改不能交错写入关系表。
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM system_users WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, user.ID, tenantID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_users SET username=?, nickname=?, remark=?, dept_id=?, post_ids=?, email=?, mobile=?, sex=?, avatar=?
		WHERE id=? AND tenant_id=? AND deleted=0`,
		user.Username, user.Nickname, user.Remark, user.DeptID, posts, user.Email, user.Mobile, user.Sex, user.Avatar, user.ID, tenantID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT post_id FROM system_user_post WHERE user_id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, user.ID, tenantID)
	if err != nil {
		return err
	}
	existing := make(map[int64]bool)
	for rows.Next() {
		var postID int64
		if err := rows.Scan(&postID); err != nil {
			rows.Close()
			return err
		}
		existing[postID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	wanted := make(map[int64]bool)
	for _, postID := range uniquePostIDs(user.PostIDs) {
		wanted[postID] = true
	}
	for postID := range existing {
		if wanted[postID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE system_user_post SET deleted=1 WHERE user_id=? AND post_id=? AND tenant_id=? AND deleted=0`, user.ID, postID, tenantID); err != nil {
			return err
		}
	}
	for postID := range wanted {
		if existing[postID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_user_post (user_id, post_id, tenant_id, deleted) VALUES (?, ?, ?, 0)`, user.ID, postID, tenantID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// userPostJSON 同步 DTO 使用的 JSON 列；nil 对应 Java 的 null，空切片对应 []。
func userPostJSON(ids []int64) (any, error) {
	if ids == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(uniquePostIDs(ids))
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func uniquePostIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (m *MySQL) UpdatePassword(ctx context.Context, tenantID, id int64, passwordHash string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET password=? WHERE id=? AND tenant_id=? AND deleted=0`, passwordHash, id, tenantID)
	return err
}

func (m *MySQL) UpdateStatus(ctx context.Context, tenantID, id int64, status int) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET status=? WHERE id=? AND tenant_id=? AND deleted=0`, status, id, tenantID)
	return err
}

func (m *MySQL) DeptExists(ctx context.Context, tenantID, id int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dept WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateDept(ctx context.Context, tenantID int64, dept Dept) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_dept
		(name, parent_id, sort, leader_user_id, phone, email, status, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		dept.Name, dept.ParentID, dept.Sort, dept.LeaderUserID, dept.Phone, dept.Email, dept.Status, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateDept(ctx context.Context, tenantID int64, dept Dept) error {
	// 锁住本租户的部门层级并在同一事务中复查，避免两个并发更新各自通过服务层校验后互设父部门。
	// 部门改父是低频管理操作；按 ID 顺序取锁可降低并发改动的死锁概率。
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, parent_id FROM system_dept
		WHERE tenant_id=? AND deleted=0 ORDER BY id FOR UPDATE`, tenantID)
	if err != nil {
		return err
	}
	parents := make(map[int64]int64)
	for rows.Next() {
		var id, parentID int64
		if err := rows.Scan(&id, &parentID); err != nil {
			_ = rows.Close()
			return err
		}
		parents[id] = parentID
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if _, exists := parents[dept.ID]; !exists {
		return &Error{Code: codeDeptNotFound, Msg: "当前部门不存在"}
	}
	if err := validateDeptParentChain(dept.ID, dept.ParentID, func(id int64) (int64, bool, error) {
		parentID, exists := parents[id]
		return parentID, exists, nil
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_dept SET name=?, parent_id=?, sort=?, leader_user_id=?, phone=?, email=?, status=?
		WHERE id=? AND tenant_id=? AND deleted=0`,
		dept.Name, dept.ParentID, dept.Sort, dept.LeaderUserID, dept.Phone, dept.Email, dept.Status, dept.ID, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

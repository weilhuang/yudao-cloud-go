package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const userDetailSelect = `SELECT u.id, u.username, u.nickname, IFNULL(u.remark,''), u.dept_id, IFNULL(d.name,''), u.post_ids,
	IFNULL(u.email,''), IFNULL(u.mobile,''), u.sex, IFNULL(u.avatar,''), u.status, IFNULL(u.login_ip,''),
	UNIX_TIMESTAMP(u.login_date)*1000, UNIX_TIMESTAMP(u.create_time)*1000
	FROM system_users u LEFT JOIN system_dept d ON d.id = u.dept_id AND d.deleted = 0 `

// UserListByIDs 按请求顺序返回本租户仍存在的用户。缺失和其它租户的编号直接略过。
func (m *MySQL) UserListByIDs(ctx context.Context, tenantID int64, ids []int64) ([]UserDetail, error) {
	if len(ids) == 0 {
		return []UserDetail{}, nil
	}
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	clause, scopeArgs := accessSQL(accessFrom(ctx))
	args := make([]any, 0, len(unique)+1+len(scopeArgs))
	args = append(args, tenantID)
	for _, id := range unique {
		args = append(args, id)
	}
	args = append(args, scopeArgs...)
	rows, err := m.DB.QueryContext(ctx, userDetailSelect+`WHERE u.deleted=0 AND u.tenant_id=? AND u.id IN (`+marks+`)`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[int64]UserDetail{}
	for rows.Next() {
		user, err := scanUserDetail(rows)
		if err != nil {
			return nil, err
		}
		found[user.ID] = user
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	list := make([]UserDetail, 0, len(unique))
	for _, id := range unique {
		if user, ok := found[id]; ok {
			list = append(list, user)
		}
	}
	return list, nil
}

// UserByNickname 按昵称模糊搜索，包含禁用用户。空关键词由调用方处理成空列表。
func (m *MySQL) UserByNickname(ctx context.Context, tenantID int64, nickname string) ([]UserSimple, error) {
	clause, scopeArgs := accessSQL(accessFrom(ctx))
	args := append([]any{tenantID, "%" + nickname + "%"}, scopeArgs...)
	rows, err := m.DB.QueryContext(ctx, `SELECT u.id, u.nickname, IFNULL(u.avatar,''), u.sex, u.dept_id, IFNULL(d.name,'')
		FROM system_users u LEFT JOIN system_dept d ON d.id = u.dept_id AND d.deleted = 0
		WHERE u.deleted=0 AND u.tenant_id=? AND u.nickname LIKE ?`+clause+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]UserSimple, 0)
	for rows.Next() {
		var user UserSimple
		var sex, dept sql.NullInt64
		if err := rows.Scan(&user.ID, &user.Nickname, &user.Avatar, &sex, &dept, &user.DeptName); err != nil {
			return nil, err
		}
		if sex.Valid {
			value := int(sex.Int64)
			user.Sex = &value
		}
		if dept.Valid {
			user.DeptID = &dept.Int64
		}
		list = append(list, user)
	}
	return list, rows.Err()
}

// UserExportRows 按用户分页的筛选和 id 倒序输出全部命中行，不受每页 200 条上限约束。
func (m *MySQL) UserExportRows(ctx context.Context, tenantID int64, query UserQuery, emit func(UserDetail) error) error {
	query, err := m.expandUserQuery(ctx, tenantID, query)
	if err != nil {
		return err
	}
	where, args := userFilter(tenantID, query)
	rows, err := m.DB.QueryContext(ctx, userDetailSelect+where+` ORDER BY u.id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		user, err := scanUserDetail(rows)
		if err != nil {
			return err
		}
		if err := emit(user); err != nil {
			return err
		}
	}
	return rows.Err()
}

// RoleExportRows 按角色分页的筛选和 sort 升序输出，不截断页大小。
func (m *MySQL) RoleExportRows(ctx context.Context, tenantID int64, name, code string, status *int, emit func(RoleDetail) error) error {
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
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code, sort, status, type, IFNULL(remark,''), data_scope, IFNULL(data_scope_dept_ids,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_role `+where+` ORDER BY sort, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		role, err := scanRoleDetail(rows)
		if err != nil {
			return err
		}
		if err := emit(role); err != nil {
			return err
		}
	}
	return rows.Err()
}

// DictLabels 读取字典值和显示名。重复 value 保留排序靠前的第一条，和岗位导出一致。
func (m *MySQL) DictLabels(ctx context.Context, dictType string) (map[int]string, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT value, label FROM system_dict_data
		WHERE dict_type=? AND deleted=0 ORDER BY sort, id`, dictType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[int]string{}
	for rows.Next() {
		var value, label string
		if err := rows.Scan(&value, &label); err != nil {
			return nil, err
		}
		number, err := strconv.Atoi(value)
		if err != nil {
			continue
		}
		if _, seen := labels[number]; !seen {
			labels[number] = label
		}
	}
	return labels, rows.Err()
}

func scanUserDetail(row interface{ Scan(...any) error }) (UserDetail, error) {
	var user UserDetail
	var dept, sex, login, created sql.NullInt64
	var posts sql.NullString
	err := row.Scan(&user.ID, &user.Username, &user.Nickname, &user.Remark, &dept, &user.DeptName, &posts,
		&user.Email, &user.Mobile, &sex, &user.Avatar, &user.Status, &user.LoginIP, &login, &created)
	if err != nil {
		return UserDetail{}, err
	}
	if posts.Valid && posts.String != "" {
		if err := json.Unmarshal([]byte(posts.String), &user.PostIDs); err != nil {
			return UserDetail{}, err
		}
	}
	if user.PostIDs == nil {
		user.PostIDs = []int64{}
	}
	if dept.Valid {
		user.DeptID = &dept.Int64
	}
	if sex.Valid {
		value := int(sex.Int64)
		user.Sex = &value
	}
	if login.Valid && login.Int64 > 0 {
		user.LoginDate = &login.Int64
	}
	if created.Valid {
		user.CreateTime = created.Int64
	}
	return user, nil
}

func excelTime(millis *int64) string {
	if millis == nil || *millis <= 0 {
		return ""
	}
	return time.UnixMilli(*millis).In(shanghai).Format("2006-01-02 15:04:05")
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

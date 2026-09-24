package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (m *MySQL) TenantByID(ctx context.Context, id int64) (*Tenant, error) {
	return scanTenant(m.DB.QueryRowContext(ctx, tenantSelect+` WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) TenantByName(ctx context.Context, name string) (*Tenant, error) {
	return scanTenant(m.DB.QueryRowContext(ctx, tenantSelect+` WHERE name=? AND deleted=0`, name))
}

func (m *MySQL) TenantByWebsite(ctx context.Context, website string) (*Tenant, error) {
	return scanTenant(m.DB.QueryRowContext(ctx, tenantSelect+` WHERE deleted=0 AND websites LIKE ?`, "%\""+website+"\"%"))
}

const tenantSelect = `SELECT id, name, IFNULL(contact_name,''), IFNULL(contact_mobile,''), status, IFNULL(websites,'[]'), package_id, IFNULL(UNIX_TIMESTAMP(expire_time),0)*1000, account_count, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_tenant`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTenant(row rowScanner) (*Tenant, error) {
	var item Tenant
	var websites string
	var expire, created sql.NullInt64
	err := row.Scan(&item.ID, &item.Name, &item.ContactName, &item.ContactMobile, &item.Status, &websites, &item.PackageID, &expire, &item.AccountCount, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(websites), &item.Websites)
	if item.Websites == nil {
		item.Websites = []string{}
	}
	if expire.Valid {
		item.ExpireTime = expire.Int64
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func (m *MySQL) TenantNameTaken(ctx context.Context, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant WHERE deleted=0 AND name=? AND id<>?`, name, exceptID))
}

func (m *MySQL) TenantWebsiteTaken(ctx context.Context, websites []string, exceptID int64) (string, error) {
	for _, website := range websites {
		var n int
		err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant WHERE deleted=0 AND id<>? AND websites LIKE ?`, exceptID, "%\""+website+"\"%").Scan(&n)
		if err != nil {
			return "", err
		}
		if n > 0 {
			return website, nil
		}
	}
	return "", nil
}

func (m *MySQL) TenantPage(ctx context.Context, pageNo, pageSize int, name, contactName string, status *int) (Page[Tenant], error) {
	where := `WHERE deleted=0`
	var args []any
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if contactName != "" {
		where += ` AND contact_name LIKE ?`
		args = append(args, "%"+contactName+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant `+where, args...).Scan(&total); err != nil {
		return Page[Tenant]{}, err
	}
	pageNo, pageSize = normalizePage(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, tenantSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[Tenant]{}, err
	}
	defer rows.Close()
	list := make([]Tenant, 0)
	for rows.Next() {
		item, err := scanTenant(rows)
		if err != nil {
			return Page[Tenant]{}, err
		}
		list = append(list, *item)
	}
	return Page[Tenant]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) TenantSimple(ctx context.Context) ([]Tenant, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name FROM system_tenant WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Tenant
	for rows.Next() {
		var item Tenant
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) CreateTenant(ctx context.Context, tenant Tenant, passwordHash string, menuIDs []int64) (int64, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	websites, _ := json.Marshal(emptyStrings(tenant.Websites))
	res, err := tx.ExecContext(ctx, `INSERT INTO system_tenant
		(name, contact_name, contact_mobile, status, websites, package_id, expire_time, account_count, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, FROM_UNIXTIME(?/1000), ?, 0, ?)`,
		tenant.Name, tenant.ContactName, tenant.ContactMobile, tenant.Status, string(websites), tenant.PackageID, tenant.ExpireTime, tenant.AccountCount, time.Now())
	if err != nil {
		return 0, err
	}
	tenantID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	roleRes, err := tx.ExecContext(ctx, `INSERT INTO system_role (name, code, sort, status, type, remark, data_scope, tenant_id, deleted, create_time)
		VALUES ('租户管理员', 'tenant_admin', 0, 0, 1, '系统自动生成', 1, ?, 0, ?)`, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	roleID, err := roleRes.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, menuID := range menuIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`, roleID, menuID, tenantID, time.Now()); err != nil {
			return 0, err
		}
	}
	userRes, err := tx.ExecContext(ctx, `INSERT INTO system_users (username, password, nickname, status, deleted, tenant_id, create_time)
		VALUES (?, ?, ?, 0, 0, ?, ?)`, tenant.Username, passwordHash, tenant.ContactName, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	userID, err := userRes.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO system_user_role (user_id, role_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`, userID, roleID, tenantID, time.Now()); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_tenant SET contact_user_id=? WHERE id=?`, userID, tenantID); err != nil {
		return 0, err
	}
	return tenantID, tx.Commit()
}

func (m *MySQL) UpdateTenant(ctx context.Context, tenant Tenant, menuIDs []int64, syncMenus bool) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	websites, _ := json.Marshal(emptyStrings(tenant.Websites))
	if _, err := tx.ExecContext(ctx, `UPDATE system_tenant SET name=?, contact_name=?, contact_mobile=?, status=?, websites=?, package_id=?, expire_time=FROM_UNIXTIME(?/1000), account_count=? WHERE id=? AND deleted=0`,
		tenant.Name, tenant.ContactName, tenant.ContactMobile, tenant.Status, string(websites), tenant.PackageID, tenant.ExpireTime, tenant.AccountCount, tenant.ID); err != nil {
		return err
	}
	if syncMenus {
		if err := syncTenantMenus(ctx, tx, tenant.ID, menuIDs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// syncTenantMenus 把租户管理员的菜单换成新套餐，并删掉其他角色超出套餐的菜单。
func syncTenantMenus(ctx context.Context, tx *sql.Tx, tenantID int64, menuIDs []int64) error {
	// 套餐更换先持有租户行锁，再按 ID 锁角色；菜单授权事务采用相同顺序。
	rows, err := tx.QueryContext(ctx, `SELECT id, code FROM system_role WHERE tenant_id=? AND deleted=0 ORDER BY id FOR UPDATE`, tenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type roleRow struct {
		id   int64
		code string
	}
	var roles []roleRow
	for rows.Next() {
		var item roleRow
		if err := rows.Scan(&item.id, &item.code); err != nil {
			return err
		}
		roles = append(roles, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	allowed := map[int64]bool{}
	for _, id := range menuIDs {
		allowed[id] = true
	}
	for _, role := range roles {
		if role.code == "tenant_admin" {
			if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, role.id, tenantID); err != nil {
				return err
			}
			for _, menuID := range menuIDs {
				if _, err := tx.ExecContext(ctx, `INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`,
					role.id, menuID, tenantID, time.Now()); err != nil {
					return err
				}
			}
			continue
		}
		current, err := queryIDs(ctx, tx, `SELECT menu_id FROM system_role_menu WHERE role_id=? AND tenant_id=? AND deleted=0`, role.id, tenantID)
		if err != nil {
			return err
		}
		var keep []int64
		for _, menuID := range current {
			if allowed[menuID] {
				keep = append(keep, menuID)
			}
		}
		if len(keep) == len(current) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, role.id, tenantID); err != nil {
			return err
		}
		for _, menuID := range keep {
			if _, err := tx.ExecContext(ctx, `INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`,
				role.id, menuID, tenantID, time.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func emptyStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func emptyInt64(items []int64) []int64 {
	if items == nil {
		return []int64{}
	}
	return items
}

func (m *MySQL) DeleteTenant(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_tenant SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func (m *MySQL) PackageByID(ctx context.Context, id int64) (*TenantPackage, error) {
	var item TenantPackage
	var menus string
	var created sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, status, IFNULL(remark,''), IFNULL(menu_ids,'[]'), UNIX_TIMESTAMP(create_time)*1000 FROM system_tenant_package WHERE id=? AND deleted=0`, id).
		Scan(&item.ID, &item.Name, &item.Status, &item.Remark, &menus, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(menus), &item.MenuIDs)
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func (m *MySQL) PackageNameTaken(ctx context.Context, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant_package WHERE deleted=0 AND name=? AND id<>?`, name, exceptID))
}

func (m *MySQL) PackageUsed(ctx context.Context, id int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant WHERE deleted=0 AND package_id=?`, id))
}

func (m *MySQL) PackagePage(ctx context.Context, pageNo, pageSize int, name string, status *int) (Page[TenantPackage], error) {
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
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_tenant_package `+where, args...).Scan(&total); err != nil {
		return Page[TenantPackage]{}, err
	}
	pageNo, pageSize = normalizePage(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, status, IFNULL(remark,''), IFNULL(menu_ids,'[]'), UNIX_TIMESTAMP(create_time)*1000 FROM system_tenant_package `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[TenantPackage]{}, err
	}
	defer rows.Close()
	list := make([]TenantPackage, 0)
	for rows.Next() {
		var item TenantPackage
		var menus string
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Remark, &menus, &created); err != nil {
			return Page[TenantPackage]{}, err
		}
		_ = json.Unmarshal([]byte(menus), &item.MenuIDs)
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return Page[TenantPackage]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) PackageSimple(ctx context.Context) ([]TenantPackage, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, status, '', '[]', 0 FROM system_tenant_package WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TenantPackage
	for rows.Next() {
		var item TenantPackage
		var menus string
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Remark, &menus, &item.CreateTime); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) CreatePackage(ctx context.Context, item TenantPackage) (int64, error) {
	raw, _ := json.Marshal(emptyInt64(item.MenuIDs))
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_tenant_package (name, status, remark, menu_ids, deleted, create_time) VALUES (?, ?, ?, ?, 0, ?)`,
		item.Name, item.Status, item.Remark, string(raw), time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdatePackage(ctx context.Context, item TenantPackage) error {
	raw, _ := json.Marshal(emptyInt64(item.MenuIDs))
	_, err := m.DB.ExecContext(ctx, `UPDATE system_tenant_package SET name=?, status=?, remark=?, menu_ids=? WHERE id=? AND deleted=0`,
		item.Name, item.Status, item.Remark, string(raw), item.ID)
	return err
}

func (m *MySQL) DeletePackage(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_tenant_package SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func (m *MySQL) NoticePage(ctx context.Context, tenantID int64, pageNo, pageSize int, title string, status *int) (Page[Notice], error) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if title != "" {
		where += ` AND title LIKE ?`
		args = append(args, "%"+title+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_notice `+where, args...).Scan(&total); err != nil {
		return Page[Notice]{}, err
	}
	pageNo, pageSize = normalizePage(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, title, IFNULL(content,''), type, status, UNIX_TIMESTAMP(create_time)*1000 FROM system_notice `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[Notice]{}, err
	}
	defer rows.Close()
	list := make([]Notice, 0)
	for rows.Next() {
		var item Notice
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Title, &item.Content, &item.Type, &item.Status, &created); err != nil {
			return Page[Notice]{}, err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return Page[Notice]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) NoticeByID(ctx context.Context, tenantID, id int64) (*Notice, error) {
	var item Notice
	var created sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT id, title, IFNULL(content,''), type, status, UNIX_TIMESTAMP(create_time)*1000 FROM system_notice WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&item.ID, &item.Title, &item.Content, &item.Type, &item.Status, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, err
}

func (m *MySQL) CreateNotice(ctx context.Context, tenantID int64, item Notice) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_notice (title, content, type, status, tenant_id, deleted, create_time) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		item.Title, item.Content, item.Type, item.Status, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateNotice(ctx context.Context, tenantID int64, item Notice) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_notice SET title=?, content=?, type=?, status=? WHERE id=? AND tenant_id=? AND deleted=0`,
		item.Title, item.Content, item.Type, item.Status, item.ID, tenantID)
	return err
}

func (m *MySQL) DeleteNotice(ctx context.Context, tenantID, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_notice SET deleted=1 WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	return err
}

func (m *MySQL) ProfilePassword(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := m.DB.QueryRowContext(ctx, `SELECT password FROM system_users WHERE id=? AND deleted=0`, userID).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

func (m *MySQL) UpdateProfile(ctx context.Context, userID int64, nickname, email, mobile, avatar string, sex *int) error {
	value := 0
	if sex != nil {
		value = *sex
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET nickname=?, email=?, mobile=?, avatar=?, sex=? WHERE id=? AND deleted=0`,
		nickname, email, mobile, avatar, value, userID)
	return err
}

func (m *MySQL) UpdateOAuthUser(ctx context.Context, tenantID, userID int64, nickname, email, mobile *string, sex *int) error {
	var exists int
	err := m.DB.QueryRowContext(ctx, `SELECT 1 FROM system_users WHERE id=? AND tenant_id=? AND deleted=0`, userID, tenantID).Scan(&exists)
	if err == sql.ErrNoRows {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	if err != nil {
		return err
	}
	if email != nil && strings.TrimSpace(*email) != "" {
		var other int64
		err = m.DB.QueryRowContext(ctx, `SELECT id FROM system_users WHERE email=? AND tenant_id=? AND deleted=0 LIMIT 1`, *email, tenantID).Scan(&other)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && other != userID {
			return &Error{Code: 1_002_003_002, Msg: "邮箱已经存在"}
		}
	}
	if mobile != nil && strings.TrimSpace(*mobile) != "" {
		var other int64
		err = m.DB.QueryRowContext(ctx, `SELECT id FROM system_users WHERE mobile=? AND tenant_id=? AND deleted=0 LIMIT 1`, *mobile, tenantID).Scan(&other)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && other != userID {
			return &Error{Code: 1_002_003_001, Msg: "手机号已经存在"}
		}
	}
	sets := make([]string, 0, 4)
	args := make([]any, 0, 6)
	if nickname != nil {
		sets = append(sets, "nickname=?")
		args = append(args, *nickname)
	}
	if email != nil {
		sets = append(sets, "email=?")
		args = append(args, *email)
	}
	if mobile != nil {
		sets = append(sets, "mobile=?")
		args = append(args, *mobile)
	}
	if sex != nil {
		sets = append(sets, "sex=?")
		args = append(args, *sex)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, userID, tenantID)
	_, err = m.DB.ExecContext(ctx, `UPDATE system_users SET `+strings.Join(sets, ", ")+` WHERE id=? AND tenant_id=? AND deleted=0`, args...)
	return err
}

func (m *MySQL) UpdateOwnPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET password=? WHERE id=? AND deleted=0`, passwordHash, userID)
	return err
}

func normalizePage(pageNo, pageSize int) (int, int) {
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

package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

func (m *MySQL) RoleByID(ctx context.Context, tenantID, id int64) (*RoleSave, error) {
	var role RoleSave
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, code, sort, status, type, IFNULL(remark,'')
		FROM system_role WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&role.ID, &role.Name, &role.Code, &role.Sort, &role.Status, &role.Type, &role.Remark)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &role, err
}

func (m *MySQL) RoleNameTaken(ctx context.Context, tenantID int64, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_role WHERE deleted=0 AND tenant_id=? AND name=? AND id<>?`, tenantID, name, exceptID))
}

func (m *MySQL) RoleCodeTaken(ctx context.Context, tenantID int64, code string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_role WHERE deleted=0 AND tenant_id=? AND code=? AND id<>?`, tenantID, code, exceptID))
}

func (m *MySQL) CreateRole(ctx context.Context, tenantID int64, role RoleSave) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_role
		(name, code, sort, status, type, remark, data_scope, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, 0, ?)`,
		role.Name, role.Code, role.Sort, role.Status, role.Type, role.Remark, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateRole(ctx context.Context, tenantID int64, role RoleSave) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_role SET name=?, code=?, sort=?, status=?, remark=? WHERE id=? AND tenant_id=? AND deleted=0`,
		role.Name, role.Code, role.Sort, role.Status, role.Remark, role.ID, tenantID)
	return err
}

// UpdateRoleDataScope 在同一事务里锁定角色和指定部门，再保存 JSON 部门集合。
// 这样并发删除或跨租户编号不能留下越界数据范围。
func (m *MySQL) UpdateRoleDataScope(ctx context.Context, tenantID, roleID int64, scope int, deptIDs []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var roleType int
	err = tx.QueryRowContext(ctx, `SELECT type FROM system_role WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, roleID, tenantID).Scan(&roleType)
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	if err != nil {
		return err
	}
	if roleType == roleTypeSystem {
		return &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
	}
	for _, id := range deptIDs {
		var existing int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM system_dept WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, id, tenantID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: 1_002_004_002, Msg: "当前部门不存在"}
		}
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(deptIDs)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_role SET data_scope=?, data_scope_dept_ids=?
		WHERE id=? AND tenant_id=? AND deleted=0 AND type<>?`, scope, string(encoded), roleID, tenantID, roleTypeSystem); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQL) DeleteRole(ctx context.Context, tenantID, id int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 与授权替换共用角色行锁：删除提交后，等待中的授权必须重新确认角色仍存在。
	var roleType int
	err = tx.QueryRowContext(ctx, `SELECT type FROM system_role WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, id, tenantID).Scan(&roleType)
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	if err != nil {
		return err
	}
	if roleType == roleTypeSystem {
		return &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_role SET deleted=1 WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
		return err
	}
	// Java processRoleDeleted 会一并逻辑删除用户角色与角色菜单关系。
	if _, err := tx.ExecContext(ctx, `UPDATE system_user_role SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteRoleList 先按编号顺序锁定并校验，再一起删除角色和两类关系。
// 中途发现不存在或内置角色时回滚，避免批删只成功一半。
func (m *MySQL) DeleteRoleList(ctx context.Context, tenantID int64, input []int64) error {
	if len(input) == 0 {
		return nil
	}
	if len(input) > 1000 {
		return &Error{Code: codeBadRequest, Msg: "请求参数过多"}
	}
	seen := make(map[int64]struct{}, len(input))
	ids := make([]int64, 0, len(input))
	for _, id := range input {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var roleType int
		err := tx.QueryRowContext(ctx, `SELECT type FROM system_role WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, id, tenantID).Scan(&roleType)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
		}
		if err != nil {
			return err
		}
		if roleType == roleTypeSystem {
			return &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
		}
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE system_role SET deleted=1 WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE system_user_role SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, id, tenantID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) MenuByID(ctx context.Context, id int64) (*MenuSave, error) {
	var menu MenuSave
	var visible, keep, always int
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, IFNULL(permission,''), type, sort, parent_id, IFNULL(path,''), IFNULL(icon,''), IFNULL(component,''), IFNULL(component_name,''), status, visible+0, keep_alive+0, always_show+0, UNIX_TIMESTAMP(create_time)*1000
		FROM system_menu WHERE id=? AND deleted=0`, id).
		Scan(&menu.ID, &menu.Name, &menu.Permission, &menu.Type, &menu.Sort, &menu.ParentID, &menu.Path, &menu.Icon, &menu.Component, &menu.ComponentName, &menu.Status, &visible, &keep, &always, &menu.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	menu.Visible, menu.KeepAlive, menu.AlwaysShow = visible != 0, keep != 0, always != 0
	return &menu, nil
}

func (m *MySQL) MenuNameTaken(ctx context.Context, parentID int64, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_menu WHERE deleted=0 AND parent_id=? AND name=? AND id<>?`, parentID, name, exceptID))
}

func (m *MySQL) MenuComponentTaken(ctx context.Context, componentName string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_menu WHERE deleted=0 AND component_name=? AND id<>?`, componentName, exceptID))
}

func (m *MySQL) MenuChildCount(ctx context.Context, id int64) (int, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_menu WHERE deleted=0 AND parent_id=?`, id).Scan(&n)
	return n, err
}

func (m *MySQL) CreateMenu(ctx context.Context, menu MenuSave) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_menu
		(name, permission, type, sort, parent_id, path, icon, component, component_name, status, visible, keep_alive, always_show, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		menu.Name, menu.Permission, menu.Type, menu.Sort, menu.ParentID, menu.Path, menu.Icon, menu.Component, menu.ComponentName, menu.Status, bit(menu.Visible), bit(menu.KeepAlive), bit(menu.AlwaysShow))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateMenu(ctx context.Context, menu MenuSave) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_menu SET name=?, permission=?, type=?, sort=?, parent_id=?, path=?, icon=?, component=?, component_name=?, status=?, visible=?, keep_alive=?, always_show=?
		WHERE id=? AND deleted=0`,
		menu.Name, menu.Permission, menu.Type, menu.Sort, menu.ParentID, menu.Path, menu.Icon, menu.Component, menu.ComponentName, menu.Status, bit(menu.Visible), bit(menu.KeepAlive), bit(menu.AlwaysShow), menu.ID)
	return err
}

func (m *MySQL) DeleteMenu(ctx context.Context, id int64) error {
	return m.deleteMenuIDs(ctx, []int64{id})
}

// DeleteMenuList 与 Java 一样在修改前检查所有子菜单，整体事务提交。
func (m *MySQL) DeleteMenuList(ctx context.Context, ids []int64) error {
	return m.deleteMenuIDs(ctx, ids)
}

func (m *MySQL) deleteMenuIDs(ctx context.Context, ids []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_menu WHERE parent_id=? AND deleted=0`, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return &Error{Code: 1_002_001_004, Msg: "存在子菜单，无法删除"}
		}
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE system_menu SET deleted=1 WHERE id=? AND deleted=0`, id); err != nil {
			return err
		}
		// Java PermissionService.processMenuDeleted 同时软删角色菜单关系。
		if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE menu_id=? AND deleted=0`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) PostByID(ctx context.Context, tenantID, id int64) (*PostSave, error) {
	var post PostSave
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, code, sort, status, IFNULL(remark,'') FROM system_post WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&post.ID, &post.Name, &post.Code, &post.Sort, &post.Status, &post.Remark)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &post, err
}

func (m *MySQL) PostNameTaken(ctx context.Context, tenantID int64, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_post WHERE deleted=0 AND tenant_id=? AND name=? AND id<>?`, tenantID, name, exceptID))
}

func (m *MySQL) PostCodeTaken(ctx context.Context, tenantID int64, code string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_post WHERE deleted=0 AND tenant_id=? AND code=? AND id<>?`, tenantID, code, exceptID))
}

func (m *MySQL) CreatePost(ctx context.Context, tenantID int64, post PostSave) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_post (name, code, sort, status, remark, tenant_id, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		post.Name, post.Code, post.Sort, post.Status, post.Remark, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdatePost(ctx context.Context, tenantID int64, post PostSave) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_post SET name=?, code=?, sort=?, status=?, remark=? WHERE id=? AND tenant_id=? AND deleted=0`,
		post.Name, post.Code, post.Sort, post.Status, post.Remark, post.ID, tenantID)
	return err
}

func (m *MySQL) DeletePost(ctx context.Context, tenantID, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_post SET deleted=1 WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	return err
}

func (m *MySQL) RoleMenuIDs(ctx context.Context, tenantID, roleID int64) ([]int64, error) {
	// 角色编号来自查询参数；关联角色和关系的租户，不能泄露其他租户的授权列表。
	return queryIDs(ctx, m.DB, `SELECT rm.menu_id FROM system_role_menu rm
		JOIN system_role r ON r.id=rm.role_id AND r.tenant_id=rm.tenant_id AND r.deleted=0
		WHERE rm.role_id=? AND rm.tenant_id=? AND rm.deleted=0`, roleID, tenantID)
}

func (m *MySQL) ReplaceRoleMenus(ctx context.Context, tenantID, roleID int64, menuIDs []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 服务层套餐过滤发生在事务外。先锁租户并复查当前套餐，避免套餐更换提交后
	// 旧请求把已撤销的菜单再次授予角色；套餐更新也按租户、角色的顺序加锁。
	var packageID int64
	err = tx.QueryRowContext(ctx, `SELECT package_id FROM system_tenant WHERE id=? AND deleted=0 FOR UPDATE`, tenantID).Scan(&packageID)
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: 1_002_015_000, Msg: "租户不存在"}
	}
	if err != nil {
		return err
	}
	allowed := make(map[int64]struct{}, len(menuIDs))
	if packageID == packageIDSystem {
		for _, menuID := range menuIDs {
			var id int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM system_menu WHERE id=? AND deleted=0`, menuID).Scan(&id)
			if err == nil {
				allowed[id] = struct{}{}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	} else {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT menu_ids FROM system_tenant_package WHERE id=? AND deleted=0`, packageID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: 1_002_016_000, Msg: "租户套餐不存在"}
		}
		if err != nil {
			return err
		}
		var packageMenuIDs []int64
		if err := json.Unmarshal([]byte(raw), &packageMenuIDs); err != nil {
			return err
		}
		for _, menuID := range packageMenuIDs {
			allowed[menuID] = struct{}{}
		}
	}
	// 保留传入顺序，但只使用锁定后套餐仍允许的菜单编号，并消除重复。
	filtered := make([]int64, 0, len(menuIDs))
	for _, menuID := range menuIDs {
		if _, ok := allowed[menuID]; !ok {
			continue
		}
		filtered = append(filtered, menuID)
		delete(allowed, menuID)
	}
	// 再锁角色，防止并发删除后重建菜单关系。
	var lockedRoleID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM system_role WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, roleID, tenantID).Scan(&lockedRoleID)
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_role_menu SET deleted=1 WHERE role_id=? AND tenant_id=? AND deleted=0`, roleID, tenantID); err != nil {
		return err
	}
	for _, menuID := range filtered {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`,
			roleID, menuID, tenantID, time.Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) UserRoleIDs(ctx context.Context, tenantID, userID int64) ([]int64, error) {
	// 同时约束用户、角色、授权关系的租户，旧脏数据也不能出现在管理接口里。
	return queryIDs(ctx, m.DB, `SELECT ur.role_id FROM system_user_role ur
		JOIN system_users u ON u.id=ur.user_id AND u.tenant_id=ur.tenant_id AND u.deleted=0
		JOIN system_role r ON r.id=ur.role_id AND r.tenant_id=ur.tenant_id AND r.deleted=0
		WHERE ur.user_id=? AND ur.tenant_id=? AND ur.deleted=0`, userID, tenantID)
}

func (m *MySQL) ReplaceUserRoles(ctx context.Context, tenantID, userID int64, roleIDs []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 按编号加锁，既与删除角色串行化，也避免不同请求以相反顺序锁多个角色。
	lockIDs := append([]int64(nil), roleIDs...)
	sort.Slice(lockIDs, func(i, j int) bool { return lockIDs[i] < lockIDs[j] })
	for _, roleID := range lockIDs {
		var lockedRoleID int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM system_role WHERE id=? AND tenant_id=? AND deleted=0 FOR UPDATE`, roleID, tenantID).Scan(&lockedRoleID)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
		}
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_user_role SET deleted=1 WHERE user_id=? AND tenant_id=? AND deleted=0`, userID, tenantID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_user_role (user_id, role_id, tenant_id, deleted, create_time) VALUES (?, ?, ?, 0, ?)`,
			userID, roleID, tenantID, time.Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func countAtLeastOne(row *sql.Row) (bool, error) {
	var n int
	err := row.Scan(&n)
	return n > 0, err
}

func queryIDs(ctx context.Context, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids, rows.Err()
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

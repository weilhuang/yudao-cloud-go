package demo01

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const contactSelect = `SELECT id, name, sex, UNIX_TIMESTAMP(birthday)*1000, description, IFNULL(avatar,''),
	UNIX_TIMESTAMP(create_time)*1000
	FROM yudao_demo01_contact`

func (s *Service) get(ctx context.Context, tenantID, id int64) (*Contact, error) {
	row := s.DB.QueryRowContext(ctx, contactSelect+` WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	item, err := scanContact(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func scanContact(row interface{ Scan(...any) error }) (*Contact, error) {
	var item Contact
	var birthday, created sql.NullInt64
	err := row.Scan(&item.ID, &item.Name, &item.Sex, &birthday, &item.Description, &item.Avatar, &created)
	if err != nil {
		return nil, err
	}
	if birthday.Valid {
		item.Birthday = birthday.Int64
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func (s *Service) insert(ctx context.Context, tenantID int64, in Save) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO yudao_demo01_contact
		(name, sex, birthday, description, avatar, deleted, tenant_id) VALUES (?,?,?,?,?,0,?)`,
		in.Name, in.Sex, mysqlTime(in.Birthday), in.Description, avatarArg(in.Avatar), tenantID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) update(ctx context.Context, tenantID int64, in Save) error {
	query := `UPDATE yudao_demo01_contact SET name=?, sex=?, birthday=?, description=?`
	args := []any{in.Name, in.Sex, mysqlTime(in.Birthday), in.Description}
	if in.Avatar != nil {
		query += `, avatar=?`
		args = append(args, *in.Avatar)
	}
	query += ` WHERE id=? AND tenant_id=? AND deleted=0`
	args = append(args, in.ID, tenantID)
	_, err := s.DB.ExecContext(ctx, query, args...)
	return err
}

func (s *Service) existingIDs(ctx context.Context, tenantID int64, ids []int64) (map[int64]bool, error) {
	if len(ids) == 0 {
		return map[int64]bool{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM yudao_demo01_contact WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	return found, rows.Err()
}

func (s *Service) softDelete(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE yudao_demo01_contact SET deleted=1 WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`)`, args...)
	return err
}

func (s *Service) page(ctx context.Context, tenantID int64, q Query) (Page, error) {
	where, args := pageWhere(tenantID, q)
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yudao_demo01_contact WHERE deleted=0`+where, args...).Scan(&total); err != nil {
		return Page{}, err
	}
	query := contactSelect + ` WHERE deleted=0` + where + ` ORDER BY id DESC`
	pageArgs := append([]any{}, args...)
	if q.PageSize > 0 {
		if q.PageNo <= 0 {
			q.PageNo = 1
		}
		query += ` LIMIT ? OFFSET ?`
		pageArgs = append(pageArgs, q.PageSize, (q.PageNo-1)*q.PageSize)
	}
	rows, err := s.DB.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	list := []Contact{}
	for rows.Next() {
		item, err := scanContact(rows)
		if err != nil {
			return Page{}, err
		}
		list = append(list, *item)
	}
	return Page{List: list, Total: total}, rows.Err()
}

func pageWhere(tenantID int64, q Query) (string, []any) {
	where := ` AND tenant_id=?`
	args := []any{tenantID}
	if q.Name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+q.Name+"%")
	}
	if q.Sex != nil {
		where += ` AND sex=?`
		args = append(args, *q.Sex)
	}
	if q.CreateFrom != "" && q.CreateTo != "" {
		where += ` AND create_time BETWEEN ? AND ?`
		args = append(args, q.CreateFrom, q.CreateTo)
	}
	return where, args
}

// sexLabels 取性别字典。同一取值只保留 sort、id 最小的那条，缺字典时导出保留原数字。
func (s *Service) sexLabels(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT value, label FROM system_dict_data
		WHERE dict_type='system_user_sex' AND deleted=0 ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[string]string{}
	for rows.Next() {
		var value, label string
		if err := rows.Scan(&value, &label); err != nil {
			return nil, err
		}
		if _, ok := labels[value]; !ok {
			labels[value] = label
		}
	}
	return labels, rows.Err()
}

func mysqlTime(millis int64) string {
	return time.UnixMilli(millis).In(shanghai()).Format("2006-01-02 15:04:05")
}

func avatarArg(avatar *string) any {
	if avatar == nil {
		return nil
	}
	return *avatar
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

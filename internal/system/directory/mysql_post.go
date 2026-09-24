package directory

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// PostGet 只读取当前租户未删除岗位，缺失时与 Java getPost 一样返回 null。
func (m *MySQL) PostGet(ctx context.Context, tenantID, id int64) (*PostDetail, error) {
	post, err := scanPostDetail(m.DB.QueryRowContext(ctx, `SELECT id, name, code, sort, status,
		IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000 FROM system_post
		WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &post, nil
}

// PostPage 的筛选、租户条件和 id 倒序与 Java PostMapper.selectPage 一致。
// 导出另走 PostExportRows 游标，避免把完整结果放进内存。
func (m *MySQL) PostPage(ctx context.Context, tenantID int64, query PostQuery) (Page[PostDetail], error) {
	if query.PageNo < 1 || query.PageSize < 1 || query.PageSize > 200 {
		return Page[PostDetail]{}, &Error{Code: codeBadRequest, Msg: "岗位分页参数不正确"}
	}
	where, args := postWhere(tenantID, query)
	result := Page[PostDetail]{List: make([]PostDetail, 0)}
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_post`+where, args...).Scan(&result.Total); err != nil {
		return Page[PostDetail]{}, err
	}
	statement := `SELECT id, name, code, sort, status, IFNULL(remark,''),
		UNIX_TIMESTAMP(create_time)*1000 FROM system_post` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, query.PageSize, (query.PageNo-1)*query.PageSize)
	rows, err := m.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return Page[PostDetail]{}, err
	}
	defer rows.Close()
	for rows.Next() {
		post, err := scanPostDetail(rows)
		if err != nil {
			return Page[PostDetail]{}, err
		}
		result.List = append(result.List, post)
	}
	if err := rows.Err(); err != nil {
		return Page[PostDetail]{}, err
	}
	return result, nil
}

// PostExportRows 从数据库游标逐条交给 XLSX 编码器，不构造整个岗位列表。
func (m *MySQL) PostExportRows(ctx context.Context, tenantID int64, query PostQuery, emit func(PostDetail) error) error {
	where, args := postWhere(tenantID, query)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code, sort, status, IFNULL(remark,''),
		UNIX_TIMESTAMP(create_time)*1000 FROM system_post`+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		post, err := scanPostDetail(rows)
		if err != nil {
			return err
		}
		if err := emit(post); err != nil {
			return err
		}
	}
	return rows.Err()
}

func postWhere(tenantID int64, query PostQuery) (string, []any) {
	where := ` WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if query.Code != "" {
		where += ` AND code LIKE ?`
		args = append(args, "%"+query.Code+"%")
	}
	if query.Name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+query.Name+"%")
	}
	if query.Status != nil {
		where += ` AND status=?`
		args = append(args, *query.Status)
	}
	return where, args
}

// PostStatusLabels 与 Java DictConvert 一样使用 common_status 的字典标签。
// Java 的 DictDataApi 包含停用字典数据，因此这里不按字典自身状态筛选。
func (m *MySQL) PostStatusLabels(ctx context.Context) (map[int]string, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT value, label FROM system_dict_data
		WHERE dict_type='common_status' AND deleted=0 ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := make(map[int]string)
	for rows.Next() {
		var value, label string
		if err := rows.Scan(&value, &label); err != nil {
			return nil, err
		}
		status, err := strconv.Atoi(value)
		if err == nil {
			// Java 按排序取匹配到的第一条，重复 value 不覆盖。
			if _, seen := labels[status]; !seen {
				labels[status] = label
			}
		}
	}
	return labels, rows.Err()
}

// DeletePostList 用单条 UPDATE 完成批量软删除，避免部分成功。
// MyBatis deleteByIds 对空集合不修改数据，对不存在编号也不报错。
func (m *MySQL) DeletePostList(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE system_post SET deleted=1 WHERE tenant_id=? AND deleted=0 AND id IN (`+strings.Join(marks, ",")+`)`, args...)
	return err
}

type postScanner interface {
	Scan(dest ...any) error
}

func scanPostDetail(row postScanner) (PostDetail, error) {
	var post PostDetail
	var created sql.NullInt64
	err := row.Scan(&post.ID, &post.Name, &post.Code, &post.Sort, &post.Status, &post.Remark, &created)
	if err != nil {
		return PostDetail{}, err
	}
	if created.Valid {
		post.CreateTime = created.Int64
	}
	return post, nil
}

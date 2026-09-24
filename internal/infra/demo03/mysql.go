package demo03

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Service) getStudent(ctx context.Context, tenantID, id int64) (*Student, error) {
	var item Student
	var birthday, created sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, name, sex, UNIX_TIMESTAMP(birthday)*1000, description, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_student WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&item.ID, &item.Name, &item.Sex, &birthday, &item.Description, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
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

func (s *Service) existingStudents(ctx context.Context, tenantID int64, ids []int64) (map[int64]bool, error) {
	if len(ids) == 0 {
		return map[int64]bool{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM yudao_demo03_student WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`)`, args...)
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

func insertStudent(ctx context.Context, tx *sql.Tx, tenantID int64, in Save) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO yudao_demo03_student (name, sex, birthday, description, deleted, tenant_id) VALUES (?,?,?,?,0,?)`,
		in.Name, in.Sex, mysqlTime(in.Birthday), in.Description, tenantID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateStudent(ctx context.Context, tx *sql.Tx, tenantID int64, in Save) error {
	_, err := tx.ExecContext(ctx, `UPDATE yudao_demo03_student SET name=?, sex=?, birthday=?, description=? WHERE id=? AND tenant_id=? AND deleted=0`,
		in.Name, in.Sex, mysqlTime(in.Birthday), in.Description, in.ID, tenantID)
	return err
}

func (s *Service) pageStudents(ctx context.Context, tenantID int64, q Query) (Page[Student], error) {
	where, args := studentWhere(tenantID, q)
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yudao_demo03_student WHERE deleted=0`+where, args...).Scan(&total); err != nil {
		return Page[Student]{}, err
	}
	query := `SELECT id, name, sex, UNIX_TIMESTAMP(birthday)*1000, description, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_student WHERE deleted=0` + where + ` ORDER BY id DESC`
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
		return Page[Student]{}, err
	}
	defer rows.Close()
	list := []Student{}
	for rows.Next() {
		var item Student
		var birthday, created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.Sex, &birthday, &item.Description, &created); err != nil {
			return Page[Student]{}, err
		}
		if birthday.Valid {
			item.Birthday = birthday.Int64
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return Page[Student]{List: list, Total: total}, rows.Err()
}

func studentWhere(tenantID int64, q Query) (string, []any) {
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
	if q.Description != "" {
		where += ` AND description=?`
		args = append(args, q.Description)
	}
	if q.CreateFrom != "" && q.CreateTo != "" {
		where += ` AND create_time BETWEEN ? AND ?`
		args = append(args, q.CreateFrom, q.CreateTo)
	}
	return where, args
}

func coursesByStudent(ctx context.Context, q rowQuery, tenantID, studentID int64) ([]Course, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, student_id, name, score, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_course WHERE student_id=? AND tenant_id=? AND deleted=0 ORDER BY id`, studentID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Course{}
	for rows.Next() {
		var item Course
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.StudentID, &item.Name, &item.Score, &created); err != nil {
			return nil, err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func gradeByStudent(ctx context.Context, q rowQuery, tenantID, studentID int64) (*Grade, error) {
	var item Grade
	var created sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, student_id, name, teacher, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_grade WHERE student_id=? AND tenant_id=? AND deleted=0 ORDER BY id DESC LIMIT 1`, studentID, tenantID).
		Scan(&item.ID, &item.StudentID, &item.Name, &item.Teacher, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func insertCourse(ctx context.Context, tx *sql.Tx, tenantID, studentID int64, item Course) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO yudao_demo03_course (student_id, name, score, deleted, tenant_id) VALUES (?,?,?,0,?)`,
		studentID, item.Name, item.Score, tenantID)
	return err
}

func updateCourseRow(ctx context.Context, tx *sql.Tx, tenantID int64, item Course) error {
	_, err := tx.ExecContext(ctx, `UPDATE yudao_demo03_course SET student_id=?, name=?, score=? WHERE id=? AND tenant_id=? AND deleted=0`,
		item.StudentID, item.Name, item.Score, item.ID, tenantID)
	return err
}

func insertGrade(ctx context.Context, tx *sql.Tx, tenantID, studentID int64, item Grade) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO yudao_demo03_grade (student_id, name, teacher, deleted, tenant_id) VALUES (?,?,?,0,?)`,
		studentID, item.Name, item.Teacher, tenantID)
	return err
}

func updateGradeRow(ctx context.Context, tx *sql.Tx, tenantID int64, item Grade) error {
	_, err := tx.ExecContext(ctx, `UPDATE yudao_demo03_grade SET student_id=?, name=?, teacher=? WHERE id=? AND tenant_id=? AND deleted=0`,
		item.StudentID, item.Name, item.Teacher, item.ID, tenantID)
	return err
}

func softDeleteStudents(ctx context.Context, tx *sql.Tx, tenantID int64, ids []int64) error {
	return softDeleteIDs(ctx, tx, `yudao_demo03_student`, `id`, tenantID, ids)
}

func softDeleteChildren(ctx context.Context, tx *sql.Tx, table string, tenantID int64, studentIDs []int64) error {
	return softDeleteIDs(ctx, tx, table, `student_id`, tenantID, studentIDs)
}

func softDeleteIDs(ctx context.Context, tx *sql.Tx, table, column string, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET deleted=1 WHERE tenant_id=? AND deleted=0 AND `+column+` IN (`+marks+`)`, args...)
	return err
}

type rowQuery interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func mysqlTime(millis int64) string {
	return time.UnixMilli(millis).In(shanghai()).Format("2006-01-02 15:04:05")
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}

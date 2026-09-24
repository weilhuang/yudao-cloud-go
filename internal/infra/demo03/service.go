package demo03

import (
	"context"
	"database/sql"
)

// Service 读写学生、课程和班级。标准和内嵌模式随学生保存子表，ERP 模式单独维护子表。
type Service struct {
	DB *sql.DB
}

func (s *Service) Create(ctx context.Context, tenantID int64, in Save) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = insertStudent(ctx, tx, tenantID, in)
		if err != nil || !in.WithChild {
			return err
		}
		for _, course := range in.Courses {
			if err := insertCourse(ctx, tx, tenantID, id, course); err != nil {
				return err
			}
		}
		if in.Grade != nil {
			return insertGrade(ctx, tx, tenantID, id, *in.Grade)
		}
		return nil
	})
	return id, err
}

func (s *Service) Update(ctx context.Context, tenantID int64, in Save) error {
	current, err := s.getStudent(ctx, tenantID, in.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeStudentMissing, "学生不存在")
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := updateStudent(ctx, tx, tenantID, in); err != nil {
			return err
		}
		if !in.WithChild {
			return nil
		}
		if err := s.syncCourses(ctx, tx, tenantID, in.ID, in.Courses); err != nil {
			return err
		}
		if in.Grade == nil {
			return nil
		}
		in.Grade.StudentID = in.ID
		if in.Grade.ID > 0 {
			return updateGradeRow(ctx, tx, tenantID, *in.Grade)
		}
		return insertGrade(ctx, tx, tenantID, in.ID, *in.Grade)
	})
}

func (s *Service) syncCourses(ctx context.Context, tx *sql.Tx, tenantID, studentID int64, next []Course) error {
	old, err := coursesByStudent(ctx, tx, tenantID, studentID)
	if err != nil {
		return err
	}
	keep := map[int64]bool{}
	for _, course := range next {
		course.StudentID = studentID
		if course.ID > 0 && containsCourse(old, course.ID) {
			keep[course.ID] = true
			if err := updateCourseRow(ctx, tx, tenantID, course); err != nil {
				return err
			}
			continue
		}
		if err := insertCourse(ctx, tx, tenantID, studentID, course); err != nil {
			return err
		}
	}
	var drop []int64
	for _, course := range old {
		if !keep[course.ID] {
			drop = append(drop, course.ID)
		}
	}
	return softDeleteIDs(ctx, tx, `yudao_demo03_course`, `id`, tenantID, drop)
}

func containsCourse(list []Course, id int64) bool {
	for _, item := range list {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *Service) Delete(ctx context.Context, tenantID, id int64) error {
	return s.DeleteList(ctx, tenantID, []int64{id})
}

// DeleteList 要求每个学生编号都存在。学生删除后，课程和班级一起软删除。
func (s *Service) DeleteList(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return biz(codeStudentMissing, "学生不存在")
	}
	found, err := s.existingStudents(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return biz(codeStudentMissing, "学生不存在")
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := softDeleteStudents(ctx, tx, tenantID, ids); err != nil {
			return err
		}
		if err := softDeleteChildren(ctx, tx, `yudao_demo03_course`, tenantID, ids); err != nil {
			return err
		}
		return softDeleteChildren(ctx, tx, `yudao_demo03_grade`, tenantID, ids)
	})
}

func (s *Service) Get(ctx context.Context, tenantID, id int64) (*Student, error) {
	return s.getStudent(ctx, tenantID, id)
}

func (s *Service) Page(ctx context.Context, tenantID int64, q Query) (Page[Student], error) {
	if q.PageNo <= 0 {
		q.PageNo = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 10
	}
	return s.pageStudents(ctx, tenantID, q)
}

func (s *Service) Courses(ctx context.Context, tenantID, studentID int64) ([]Course, error) {
	return coursesByStudent(ctx, s.DB, tenantID, studentID)
}

func (s *Service) GradeByStudent(ctx context.Context, tenantID, studentID int64) (*Grade, error) {
	return gradeByStudent(ctx, s.DB, tenantID, studentID)
}

func (s *Service) PageCourses(ctx context.Context, tenantID, studentID int64, pageNo, pageSize int) (Page[Course], error) {
	all, err := s.Courses(ctx, tenantID, studentID)
	if err != nil {
		return Page[Course]{}, err
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return pageSlice(all, pageNo, pageSize), nil
}

func (s *Service) CreateCourse(ctx context.Context, tenantID int64, item Course) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO yudao_demo03_course (student_id, name, score, deleted, tenant_id) VALUES (?,?,?,0,?)`,
			item.StudentID, item.Name, item.Score, tenantID)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

func (s *Service) UpdateCourse(ctx context.Context, tenantID int64, item Course) error {
	current, err := s.GetCourse(ctx, tenantID, item.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeCourseMissing, "学生课程不存在")
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return updateCourseRow(ctx, tx, tenantID, item) })
}

func (s *Service) DeleteCourse(ctx context.Context, tenantID, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return softDeleteIDs(ctx, tx, `yudao_demo03_course`, `id`, tenantID, []int64{id})
	})
}

func (s *Service) DeleteCourseList(ctx context.Context, tenantID int64, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return softDeleteIDs(ctx, tx, `yudao_demo03_course`, `id`, tenantID, ids)
	})
}

func (s *Service) GetCourse(ctx context.Context, tenantID, id int64) (*Course, error) {
	var item Course
	var created sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, student_id, name, score, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_course WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&item.ID, &item.StudentID, &item.Name, &item.Score, &created)
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

func (s *Service) PageGrades(ctx context.Context, tenantID, studentID int64, pageNo, pageSize int) (Page[Grade], error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, student_id, name, teacher, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_grade WHERE student_id=? AND tenant_id=? AND deleted=0 ORDER BY id DESC`, studentID, tenantID)
	if err != nil {
		return Page[Grade]{}, err
	}
	defer rows.Close()
	all := []Grade{}
	for rows.Next() {
		var item Grade
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.StudentID, &item.Name, &item.Teacher, &created); err != nil {
			return Page[Grade]{}, err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return Page[Grade]{}, err
	}
	return pageSlice(all, pageNo, pageSize), nil
}

func (s *Service) CreateGrade(ctx context.Context, tenantID int64, item Grade) (int64, error) {
	current, err := gradeByStudent(ctx, s.DB, tenantID, item.StudentID)
	if err != nil {
		return 0, err
	}
	if current != nil {
		return 0, biz(codeGradeExists, "学生班级已存在")
	}
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO yudao_demo03_grade (student_id, name, teacher, deleted, tenant_id) VALUES (?,?,?,0,?)`,
			item.StudentID, item.Name, item.Teacher, tenantID)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

func (s *Service) UpdateGrade(ctx context.Context, tenantID int64, item Grade) error {
	current, err := s.GetGrade(ctx, tenantID, item.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeGradeMissing, "学生班级不存在")
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return updateGradeRow(ctx, tx, tenantID, item) })
}

func (s *Service) DeleteGrade(ctx context.Context, tenantID, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return softDeleteIDs(ctx, tx, `yudao_demo03_grade`, `id`, tenantID, []int64{id})
	})
}

func (s *Service) DeleteGradeList(ctx context.Context, tenantID int64, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return softDeleteIDs(ctx, tx, `yudao_demo03_grade`, `id`, tenantID, ids)
	})
}

func (s *Service) GetGrade(ctx context.Context, tenantID, id int64) (*Grade, error) {
	var item Grade
	var created sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, student_id, name, teacher, UNIX_TIMESTAMP(create_time)*1000
		FROM yudao_demo03_grade WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
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

func (s *Service) SexLabels(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT value, label FROM system_dict_data WHERE dict_type='system_user_sex' AND deleted=0 ORDER BY sort, id`)
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

func (s *Service) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func pageSlice[T any](all []T, pageNo, pageSize int) Page[T] {
	if all == nil {
		all = []T{}
	}
	if pageSize <= 0 {
		return Page[T]{List: all, Total: int64(len(all))}
	}
	if pageNo <= 0 {
		pageNo = 1
	}
	start := (pageNo - 1) * pageSize
	if start > len(all) {
		start = len(all)
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return Page[T]{List: all[start:end], Total: int64(len(all))}
}

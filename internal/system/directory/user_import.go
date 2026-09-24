package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	codeImportEmpty    = 1_002_003_004
	codeImportPassword = 1_002_003_009
	initPasswordKey    = "system.user.init-password"
)

var (
	usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]{4,30}$`)
	mobilePattern   = regexp.MustCompile(`^(?:(?:\+|00)86)?1(?:(?:3[\d])|(?:4[0,1,4-9])|(?:5[0-3,5-9])|(?:6[2,5-7])|(?:7[0-8])|(?:8[\d])|(?:9[0-3,5-9]))\d{8}$`)
)

// ImportUser 是导入模板中的一行。性别和状态在读表时已按字典标签换成数值。
type ImportUser struct {
	Username string
	Nickname string
	DeptID   *int64
	Email    string
	Mobile   string
	Sex      *int
	Status   *int
}

// ImportFailure 保留表格中的失败顺序。用户名为空时 key 是“第 N 行”。
type ImportFailure struct {
	Key    string
	Reason string
}

// ImportResult 对齐 UserImportRespVO。失败项按出现顺序序列化。
type ImportResult struct {
	CreateUsernames []string        `json:"createUsernames"`
	UpdateUsernames []string        `json:"updateUsernames"`
	Failures        []ImportFailure `json:"-"`
}

func (r ImportResult) MarshalJSON() ([]byte, error) {
	if r.CreateUsernames == nil {
		r.CreateUsernames = []string{}
	}
	if r.UpdateUsernames == nil {
		r.UpdateUsernames = []string{}
	}
	buf := strings.Builder{}
	buf.WriteString(`{"createUsernames":`)
	raw, err := json.Marshal(r.CreateUsernames)
	if err != nil {
		return nil, err
	}
	buf.Write(raw)
	buf.WriteString(`,"updateUsernames":`)
	raw, err = json.Marshal(r.UpdateUsernames)
	if err != nil {
		return nil, err
	}
	buf.Write(raw)
	buf.WriteString(`,"failureUsernames":{`)
	for i, item := range r.Failures {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(item.Key)
		if err != nil {
			return nil, err
		}
		reason, err := json.Marshal(item.Reason)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(reason)
	}
	buf.WriteString(`}}`)
	return []byte(buf.String()), nil
}

// ImportUsers 先检查初始密码，再在一个事务里逐行校验。
// 字段或业务校验失败只记入 failureUsernames；插入过程中的数据库错误回滚全部已写行。
func (s *Service) ImportUsers(ctx context.Context, tenantID int64, rows []ImportUser, updateSupport bool) (ImportResult, error) {
	if len(rows) == 0 {
		return ImportResult{}, &Error{Code: codeImportEmpty, Msg: "导入用户数据不能为空！"}
	}
	return s.Writer.ImportUsers(ctx, tenantID, rows, updateSupport, func(password string) (string, error) {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost())
		if err != nil {
			return "", err
		}
		return string(hash), nil
	})
}

func (m *MySQL) ImportUsers(ctx context.Context, tenantID int64, rows []ImportUser, updateSupport bool, hash func(string) (string, error)) (ImportResult, error) {
	if len(rows) == 0 {
		return ImportResult{}, &Error{Code: codeImportEmpty, Msg: "导入用户数据不能为空！"}
	}
	password, err := m.initPassword(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	if strings.TrimSpace(password) == "" {
		return ImportResult{}, &Error{Code: codeImportPassword, Msg: "初始密码不能为空"}
	}
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()
	result := ImportResult{CreateUsernames: []string{}, UpdateUsernames: []string{}}
	var passwordHash string
	for i, row := range rows {
		key, reason, ok := importFieldError(row, i+1, password)
		if !ok {
			result.Failures = append(result.Failures, ImportFailure{Key: key, Reason: reason})
			continue
		}
		if reason, ok, err = m.importDeptOK(ctx, tx, tenantID, row.DeptID); err != nil {
			return ImportResult{}, err
		} else if !ok {
			result.Failures = append(result.Failures, ImportFailure{Key: row.Username, Reason: reason})
			continue
		}
		if reason, ok, err = m.importUnique(ctx, tx, tenantID, row); err != nil {
			return ImportResult{}, err
		} else if !ok {
			result.Failures = append(result.Failures, ImportFailure{Key: row.Username, Reason: reason})
			continue
		}
		var existing int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM system_users WHERE username=? AND tenant_id=? AND deleted=0`, row.Username, tenantID).Scan(&existing)
		if err == sql.ErrNoRows {
			if passwordHash == "" {
				passwordHash, err = hash(password)
				if err != nil {
					return ImportResult{}, err
				}
			}
			if err = insertImportedUser(ctx, tx, tenantID, row, passwordHash); err != nil {
				return ImportResult{}, err
			}
			result.CreateUsernames = append(result.CreateUsernames, row.Username)
			continue
		}
		if err != nil {
			return ImportResult{}, err
		}
		if !updateSupport {
			result.Failures = append(result.Failures, ImportFailure{Key: row.Username, Reason: "用户账号已经存在"})
			continue
		}
		if err = updateImportedUser(ctx, tx, tenantID, existing, row); err != nil {
			return ImportResult{}, err
		}
		result.UpdateUsernames = append(result.UpdateUsernames, row.Username)
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func (m *MySQL) initPassword(ctx context.Context) (string, error) {
	var value sql.NullString
	err := m.DB.QueryRowContext(ctx, `SELECT value FROM infra_config WHERE config_key=? AND deleted=0 LIMIT 1`, initPasswordKey).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value.String, nil
}

func importFieldError(row ImportUser, index int, password string) (string, string, bool) {
	key := row.Username
	if strings.TrimSpace(key) == "" {
		key = fmt.Sprintf("第 %d 行", index)
	}
	var msgs []string
	if strings.TrimSpace(row.Username) == "" {
		msgs = append(msgs, "username: 用户账号不能为空")
	} else if !usernamePattern.MatchString(row.Username) {
		msgs = append(msgs, "username: 用户账号由 数字、字母 组成")
		if len(row.Username) < 4 || len(row.Username) > 30 {
			msgs = append(msgs, "username: 用户账号长度为 4-30 个字符")
		}
	}
	if utf8.RuneCountInString(row.Nickname) > 30 {
		msgs = append(msgs, "nickname: 用户昵称长度不能超过30个字符")
	}
	if row.Email != "" {
		if len(row.Email) > 50 {
			msgs = append(msgs, "email: 邮箱长度不能超过 50 个字符")
		}
		if _, err := mail.ParseAddress(row.Email); err != nil {
			msgs = append(msgs, "email: 邮箱格式不正确")
		}
	}
	if row.Mobile != "" && !mobilePattern.MatchString(row.Mobile) {
		msgs = append(msgs, "mobile: 手机号格式不正确")
	}
	if n := len(password); n < 4 || n > 16 {
		msgs = append(msgs, "password: 密码长度为 4-16 位")
	}
	if len(msgs) == 0 {
		return key, "", true
	}
	return key, strings.Join(msgs, ", "), false
}

func (m *MySQL) importDeptOK(ctx context.Context, tx *sql.Tx, tenantID int64, deptID *int64) (string, bool, error) {
	if deptID == nil {
		return "当前部门不存在", false, nil
	}
	var name string
	var status int
	err := tx.QueryRowContext(ctx, `SELECT name, status FROM system_dept WHERE id=? AND tenant_id=? AND deleted=0`, *deptID, tenantID).Scan(&name, &status)
	if err == sql.ErrNoRows {
		return "当前部门不存在", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if status != 0 {
		return fmt.Sprintf("部门(%s)不处于开启状态，不允许选择", name), false, nil
	}
	return "", true, nil
}

func (m *MySQL) importUnique(ctx context.Context, tx *sql.Tx, tenantID int64, row ImportUser) (string, bool, error) {
	if row.Mobile != "" {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM system_users WHERE mobile=? AND tenant_id=? AND deleted=0 LIMIT 1`, row.Mobile, tenantID).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return "", false, err
		}
		if err == nil {
			return "手机号已经存在", false, nil
		}
	}
	if row.Email != "" {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM system_users WHERE email=? AND tenant_id=? AND deleted=0 LIMIT 1`, row.Email, tenantID).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return "", false, err
		}
		if err == nil {
			return "邮箱已经存在", false, nil
		}
	}
	return "", true, nil
}

func insertImportedUser(ctx context.Context, tx *sql.Tx, tenantID int64, row ImportUser, passwordHash string) error {
	status := any(nil)
	if row.Status != nil {
		status = *row.Status
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO system_users
		(username, password, nickname, dept_id, post_ids, email, mobile, sex, status, deleted, tenant_id, create_time)
		VALUES (?, ?, ?, ?, '[]', ?, ?, ?, ?, 0, ?, NOW())`,
		row.Username, passwordHash, row.Nickname, row.DeptID, row.Email, row.Mobile, row.Sex, status, tenantID)
	return err
}

func updateImportedUser(ctx context.Context, tx *sql.Tx, tenantID, id int64, row ImportUser) error {
	_, err := tx.ExecContext(ctx, `UPDATE system_users SET nickname=?, dept_id=?, email=?, mobile=?, sex=IFNULL(?, sex), status=IFNULL(?, status)
		WHERE id=? AND tenant_id=? AND deleted=0`,
		row.Nickname, row.DeptID, row.Email, row.Mobile, row.Sex, row.Status, id, tenantID)
	return err
}

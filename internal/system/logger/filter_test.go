package logger

import "testing"

func TestLoginFilterSuccessAndFailure(t *testing.T) {
	yes := true
	where, _ := loginFilter(1, LoginQuery{Success: &yes, Username: "admin"})
	if !containsAll(where, "result=0", "username LIKE") {
		t.Fatal(where)
	}
	no := false
	where, _ = loginFilter(1, LoginQuery{Success: &no})
	if !containsAll(where, "result>0") || containsAll(where, "result=0") {
		t.Fatal(where)
	}
}

func TestOperateFilterSkipsEmptyIDs(t *testing.T) {
	where, args := operateFilter(1, OperateQuery{Type: "用户"})
	if containsAll(where, "user_id", "biz_id") {
		t.Fatal(where)
	}
	if !containsAll(where, "o.type LIKE") || len(args) != 2 {
		t.Fatal(where, args)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, part string) bool {
	return len(part) == 0 || (len(text) >= len(part) && (func() bool {
		for i := 0; i+len(part) <= len(text); i++ {
			if text[i:i+len(part)] == part {
				return true
			}
		}
		return false
	})())
}

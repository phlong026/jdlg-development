package main

import "testing"

func TestGuardLocalOnly(t *testing.T) {
	cases := []struct {
		name    string
		dsn     string
		env     string
		wantErr bool
	}{
		{"本机 IP", "root:x@tcp(127.0.0.1:13306)/db", "development", false},
		{"localhost", "root:x@tcp(localhost:3306)/db", "", false},
		{"线上主机", "root:x@tcp(10.0.0.8:3306)/db", "development", true},
		{"云数据库域名", "root:x@tcp(rm-xxxx.mysql.rds.aliyuncs.com:3306)/db", "development", true},
		{"本机但 ENV=production", "root:x@tcp(127.0.0.1:3306)/db", "production", true},
		{"ENV 大小写不敏感", "root:x@tcp(127.0.0.1:3306)/db", "Production", true},
		{"无法解析的 DSN", "not-a-dsn", "development", true},
		{"空 DSN", "", "development", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := guardLocalOnly(c.dsn, c.env)
			if (err != nil) != c.wantErr {
				t.Fatalf("guardLocalOnly(%q, %q) err=%v, wantErr=%v", c.dsn, c.env, err, c.wantErr)
			}
		})
	}
}

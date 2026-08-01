// Package svc 的 JSON 列查询辅助：按 DB dialect 切换 MySQL / SQLite 的 JSON 函数。
//
// streaming upsert 用 metadata.stream_key 做幂等定位，需读 JSON 字段。
// MySQL 用 JSON_UNQUOTE(JSON_EXTRACT(col, '$.k'))，SQLite 用 json_extract(col, '$.k')。
package svc

import "gorm.io/gorm"

// streamKeyClause 返回按 dialect 的 stream_key WHERE 子句 + 参数（仅 JSON 部分，不含 msg_type）。
//
// MySQL:  JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.stream_key')) = ?
// SQLite: json_extract(metadata, '$.stream_key') = ?
func streamKeyClause(db *gorm.DB, key string) (string, string) {
	if dialectIsSQLite(db) {
		return "json_extract(metadata, '$.stream_key') = ?", key
	}
	// MySQL 默认（含未知 dialect 也走 MySQL 语法，生产用 MySQL）。
	return "JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.stream_key')) = ?", key
}

// dialectIsSQLite 判断当前 db 是否 SQLite。
func dialectIsSQLite(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "sqlite"
}

// dialectIsMySQL 判断当前 db 是否 MySQL。
func dialectIsMySQL(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "mysql"
}

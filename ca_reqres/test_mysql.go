//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := "emqIiIjEB3s7HKTE@tcp(mysql6.sqlpub.com:3311)/oneapi_aganhui"

	fmt.Println("正在连接 MySQL...")
	fmt.Printf("DSN: %s

", dsn)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Printf("[FAIL] 打开连接失败: %v
", err)
		return
	}
	defer db.Close()

	db.SetConnMaxLifetime(10 * time.Second)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	start := time.Now()
	if err := db.Ping(); err != nil {
		fmt.Printf("[FAIL] Ping 失败: %v
", err)
		return
	}
	elapsed := time.Since(start)

	fmt.Printf("[OK] 连接成功！耗时: %v
", elapsed)

	// 查询服务器版本
	var version string
	if err := db.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		fmt.Printf("[WARN] 查询版本失败: %v
", err)
	} else {
		fmt.Printf("[OK] MySQL 版本: %s
", version)
	}

	// 查询当前数据库
	var dbName string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&dbName); err != nil {
		fmt.Printf("[WARN] 查询数据库名失败: %v
", err)
	} else {
		fmt.Printf("[OK] 当前数据库: %s
", dbName)
	}
}

